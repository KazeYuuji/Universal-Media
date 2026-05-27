package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Layer 1: Rate Limiter (in-memory token bucket per IP)
type rateLimiter struct {
	mu        sync.Mutex
	visitors  map[string]*visitor
	rate      int
	burst     int
}

type visitor struct {
	tokens    float64
	lastSeen  time.Time
}

func newRateLimiter(rate, burst int) *rateLimiter {
	return &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		burst:    burst,
	}
}

func (rl *rateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	now := time.Now()

	if !exists {
		rl.visitors[ip] = &visitor{tokens: float64(rl.burst - 1), lastSeen: now}
		rl.cleanup()
		return true
	}

	elapsed := now.Sub(v.lastSeen).Seconds()
	v.tokens += elapsed * float64(rl.rate)
	if v.tokens > float64(rl.burst) {
		v.tokens = float64(rl.burst)
	}
	v.lastSeen = now

	if v.tokens >= 1 {
		v.tokens--
		return true
	}
	return false
}

func (rl *rateLimiter) cleanup() {
	cutoff := time.Now().Add(-5 * time.Minute)
	for ip, v := range rl.visitors {
		if v.lastSeen.Before(cutoff) {
			delete(rl.visitors, ip)
		}
	}
}

// Layer 2: SSRF Prevention — block private/internal IPs
var privateCIDRs []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8",    // localhost
		"10.0.0.0/8",      // private
		"172.16.0.0/12",   // private
		"192.168.0.0/16",  // private
		"169.254.0.0/16",  // link-local
		"::1/128",         // localhost IPv6
		"fc00::/7",        // unique local IPv6
		"fe80::/10",       // link-local IPv6
		"0.0.0.0/8",       // current network
		"100.64.0.0/10",   // carrier-grade NAT
		"198.18.0.0/15",   // benchmark testing
	} {
		_, cidrNet, err := net.ParseCIDR(cidr)
		if err == nil {
			privateCIDRs = append(privateCIDRs, cidrNet)
		}
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, cidr := range privateCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func validateProxyURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("only http/https allowed")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("empty host")
	}
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host")
	}
	for _, ip := range ips {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			continue
		}
		if isPrivateIP(parsedIP) {
			return fmt.Errorf("private IP not allowed")
		}
	}
	return nil
}

// Layer 3: Input Sanitization
var dangerousPatterns = []string{
	";", "|", "`", "$(", "${", "&&", "||",
}

func isDangerousInput(input string) bool {
	lower := strings.ToLower(input)
	for _, p := range dangerousPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func validateURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL is required")
	}
	if len(rawURL) > 2048 {
		return fmt.Errorf("URL too long")
	}
	if isDangerousInput(rawURL) {
		return fmt.Errorf("invalid characters in URL")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("only http/https URLs allowed")
	}
	if parsed.Host == "" {
		return fmt.Errorf("URL has no host")
	}
	return nil
}

// Layer 4: Security Headers
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "interest-cohort=()")
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https: data:; media-src 'self' https:; connect-src 'self' https:; frame-ancestors 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// Layer 5: Rate Limiting Middleware
var (
	apiRateLimiter   = newRateLimiter(30, 60)   // 30 req/s burst 60 for API
	proxyRateLimiter = newRateLimiter(10, 20)   // 10 req/s burst 20 for proxy
)

func rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := strings.Split(r.RemoteAddr, ":")[0]
		if ip == "" {
			ip = "unknown"
		}
		var allowed bool
		if strings.HasPrefix(r.URL.Path, "/api/proxy") {
			allowed = proxyRateLimiter.Allow(ip)
		} else {
			allowed = apiRateLimiter.Allow(ip)
		}
		if !allowed {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Layer 6: Panic Recovery
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[PANIC] %s %s: %v", r.Method, r.URL.Path, rec)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Layer 7: Request Logging
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ip := strings.Split(r.RemoteAddr, ":")[0]
		q := r.URL.RawQuery
		if len(q) > 200 {
			q = q[:200] + "..."
		}
		defer func() {
			duration := time.Since(start)
			log.Printf("[%s] %s %s %s (%s)", ip, r.Method, r.URL.Path, q, duration.Round(time.Millisecond))
		}()
		next.ServeHTTP(w, r)
	})
}
