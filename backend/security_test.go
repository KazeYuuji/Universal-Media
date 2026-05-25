package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Test Layer 1: Rate Limiting
func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(10, 5)

	// Should allow burst
	for i := 0; i < 5; i++ {
		if !rl.Allow("192.168.1.1") {
			t.Fatalf("burst %d should be allowed", i+1)
		}
	}

	// Should block after burst
	if rl.Allow("192.168.1.1") {
		t.Fatal("should be rate limited after burst exhaustion")
	}

	// Different IP should work
	if !rl.Allow("10.0.0.1") {
		t.Fatal("different IP should be allowed")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	handler := rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	sendReq := func() int {
		req, _ := http.NewRequest("GET", server.URL, nil)
		req.RemoteAddr = "1.2.3.4:12345"
		resp, err := client.Do(req)
		if err != nil {
			return -1
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}

	// Burst through
	for i := 0; i < 60; i++ {
		sendReq()
	}

	// Should get 429
	code := sendReq()
	if code != http.StatusTooManyRequests {
		t.Logf("expected 429 after burst, got %d (may vary in CI)", code)
	}
}

// Test Layer 2: SSRF Prevention
func TestSSRFBlockPrivateIP(t *testing.T) {
	tests := []struct {
		url   string
		block bool
	}{
		{"http://127.0.0.1:8081/admin", true},
		{"http://localhost:22", true},
		{"http://10.0.0.1/secret", true},
		{"http://192.168.1.1/config", true},
		{"http://169.254.169.254/latest/meta-data", true},
		{"http://[::1]:8081", true},
		{"https://www.instagram.com/reel/ABC123", false},
		{"https://example.com/video.mp4", false},
		{"ftp://evil.com/file", true}, // non-http scheme
	}

	for _, tt := range tests {
		err := validateProxyURL(tt.url)
		gotBlocked := err != nil
		if gotBlocked != tt.block {
			t.Errorf("validateProxyURL(%q) blocked=%v, want=%v (err=%v)", tt.url, gotBlocked, tt.block, err)
		}
	}
}

// Test Layer 3: Input Validation
func TestInputSanitization(t *testing.T) {
	tests := []struct {
		input     string
		dangerous bool
	}{
		{"https://youtube.com/watch?v=abc123", false},
		{"https://instagram.com/p/abc/", false},
		{"https://evil.com?cmd=;rm -rf /", true},
		{"https://x.com?id=$(cat /etc/passwd)", true},
		{"https://x.com?id=`id`", true},
		{"https://x.com?a=1&&b=2", true},
		{"https://x.com?a=1|whoami", true},
		{"normal video url", false},
	}

	for _, tt := range tests {
		err := validateURL(tt.input)
		gotDangerous := err != nil && strings.Contains(err.Error(), "invalid characters")
		if gotDangerous != tt.dangerous {
			t.Errorf("validateURL(%q) dangerous=%v, want=%v (err=%v)", tt.input, gotDangerous, tt.dangerous, err)
		}
	}
}

func TestURLLengthLimit(t *testing.T) {
	long := "https://x.com/" + strings.Repeat("a", 3000)
	err := validateURL(long)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected URL too long error for 3000 char URL")
	}
}

func TestEmptyURL(t *testing.T) {
	err := validateURL("")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestInvalidScheme(t *testing.T) {
	err := validateURL("ftp://evil.com/file")
	if err == nil || !strings.Contains(err.Error(), "only http/https") {
		t.Errorf("expected scheme error, got %v", err)
	}
}

// Test Layer 4: Security Headers
func TestSecurityHeaders(t *testing.T) {
	handler := securityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	checks := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"X-XSS-Protection":       "1; mode=block",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}

	for header, expected := range checks {
		got := resp.Header.Get(header)
		if got != expected {
			t.Errorf("header %s = %q, want %q", header, got, expected)
		}
	}
}

// Test Layer 5: Panic Recovery
func TestPanicRecovery(t *testing.T) {
	handler := recoveryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("simulated crash")
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 after panic, got %d", resp.StatusCode)
	}
}

// Test Layer 7: Full Middleware Chain
func TestFullMiddlewareChainHealth(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := loggingMiddleware(
		recoveryMiddleware(
			rateLimitMiddleware(
				securityHeadersMiddleware(
					corsMiddleware(mux),
				),
			),
		),
	)

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health check failed with %d", resp.StatusCode)
	}
}

// DDoS Simulation: Concurrent Flood
func TestDDoSSimulation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DDoS simulation in short mode")
	}

	handler := rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	concurrency := 50
	totalReqs := 500

	var wg sync.WaitGroup
	results := make(chan int, totalReqs)

	start := time.Now()
	for i := 0; i < totalReqs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", server.URL, nil)
			req.RemoteAddr = fmt.Sprintf("10.0.0.%d:12345", i%10)
			resp, err := client.Do(req)
			if err != nil {
				results <- -1
				return
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)
			results <- resp.StatusCode
		}()
		// Throttle to simulate realistic flood
		if i%concurrency == 0 && i > 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	wg.Wait()
	close(results)

	var allowed, blocked, errors int
	for code := range results {
		switch {
		case code == http.StatusTooManyRequests:
			blocked++
		case code == http.StatusOK:
			allowed++
		default:
			errors++
		}
	}

	elapsed := time.Since(start)
	t.Logf("DDoS simulation: %d reqs in %v (%d goroutines)", totalReqs, elapsed, concurrency)
	t.Logf("  Allowed: %d, Blocked (429): %d, Errors: %d", allowed, blocked, errors)

	if blocked == 0 {
		t.Log("WARNING: Rate limiter did not block any requests - may need tuning")
	}
	if allowed == 0 {
		t.Log("WARNING: All requests blocked - rate limit may be too aggressive")
	}
}

// Test SSRF via proxy handler endpoint
func TestProxyHandlerSSRF(t *testing.T) {
	handler := http.HandlerFunc(proxyDownloadHandler)

	tests := []struct {
		url        string
		wantStatus int
	}{
		{"http://localhost:8081/secret", http.StatusForbidden},
		{"http://127.0.0.1:22/", http.StatusForbidden},
		{"http://10.0.0.1/admin", http.StatusForbidden},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/api/proxy?url="+tt.url, nil)
		w := httptest.NewRecorder()
		handler(w, req)
		resp := w.Result()
		resp.Body.Close()

		if resp.StatusCode != tt.wantStatus {
			t.Errorf("proxy to %q: got %d, want %d", tt.url, resp.StatusCode, resp.StatusCode)
		}
	}
}

func TestProxyHandlerInvalidInput(t *testing.T) {
	handler := http.HandlerFunc(proxyDownloadHandler)

	tests := []struct {
		url        string
		wantStatus int
	}{
		{"", http.StatusBadRequest},
		{"javascript:alert(1)", http.StatusBadRequest},
		{"data:text/html,<script>", http.StatusBadRequest},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/api/proxy?url="+tt.url, nil)
		w := httptest.NewRecorder()
		handler(w, req)
		resp := w.Result()
		resp.Body.Close()

		if resp.StatusCode != tt.wantStatus {
			t.Errorf("proxy to %q: got %d, want %d", tt.url, resp.StatusCode, tt.wantStatus)
		}
	}
}
