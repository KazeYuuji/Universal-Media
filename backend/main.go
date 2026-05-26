package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type MediaOption struct {
	Quality  string `json:"quality"`
	Format   string `json:"format"`
	Size     string `json:"size"`
	URL      string `json:"url"`
	AudioURL string `json:"audioUrl,omitempty"`
	YtFormat string `json:"ytFormat,omitempty"`
}

type EventMessage struct {
	Status    string        `json:"status"`
	Progress  int           `json:"progress"`
	MediaType string        `json:"mediaType,omitempty"`
	Title     string        `json:"title,omitempty"`
	Duration  string        `json:"duration,omitempty"`
	Thumbnail string        `json:"thumbnail,omitempty"`
	Options   []MediaOption `json:"options,omitempty"`
	MediaURL  string        `json:"mediaUrl,omitempty"`
	Error     string        `json:"error,omitempty"`
}

func init() {
	net.DefaultResolver.PreferGo = true
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, Content-Length, Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func formatDuration(seconds float64) string {
	if seconds <= 0 {
		return "Live"
	}
	mins := int(seconds) / 60
	secs := int(seconds) % 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}

func detectExtFromURL(rawURL string, defaultExt string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return defaultExt
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(parsed.Path), "."))
	if ext != "" {
		return ext
	}
	if strings.Contains(strings.ToLower(parsed.RawQuery), "mime=video") {
		return "mp4"
	}
	return defaultExt
}

func decodeHTMLEntities(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	return s
}

func extractMetaContent(html string, property string) string {
	patterns := []string{
		`(?i)<meta[^>]+(?:property|name)=["']` + regexp.QuoteMeta(property) + `["'][^>]+content=["']([^"']+)["']`,
		`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+(?:property|name)=["']` + regexp.QuoteMeta(property) + `["']`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(html)
		if len(matches) > 1 {
			return decodeHTMLEntities(strings.TrimSpace(matches[1]))
		}
	}
	return ""
}

func extractFirstMeta(html string, keys ...string) string {
	for _, key := range keys {
		if v := extractMetaContent(html, key); v != "" {
			return v
		}
	}
	return ""
}

func extractEmbeddedImageURLs(html string) []string {
	seen := make(map[string]bool)
	var urls []string
	patterns := []string{
		`"display_url"\s*:\s*"([^"]+)"`,
		`"url"\s*:\s*"(https://[^"]+\.(?:jpg|jpeg|png|webp)[^"]*)"`,
		`"image"\s*:\s*\{\s*"uri"\s*:\s*"([^"]+)"`,
		`"imageURL"\s*:\s*\{[^}]*"urlList"\s*:\s*\[\s*"(https://[^"]+)"`,
		`"src"\s*:\s*"(https://[^"]+scontent[^"]+)"`,
		`"src"\s*:\s*"(https://[^"]+fbcdn[^"]+)"`,
		`"src"\s*:\s*"(https://[^"]+tiktokcdn[^"]+)"`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		for _, m := range re.FindAllStringSubmatch(html, -1) {
			if len(m) < 2 {
				continue
			}
			u := decodeHTMLEntities(strings.ReplaceAll(m[1], `\u0026`, "&"))
			if u == "" || seen[u] || strings.Contains(u, "emoji") {
				continue
			}
			seen[u] = true
			urls = append(urls, u)
		}
	}
	return urls
}

func platformReferer(platform string) string {
	switch platform {
	case "Instagram":
		return "https://www.instagram.com/"
	case "TikTok":
		return "https://www.tiktok.com/"
	case "Facebook":
		return "https://www.facebook.com/"
	case "YouTube":
		return "https://www.youtube.com/"
	default:
		return ""
	}
}

func fetchPageHTML(pageURL string, referer string) (string, error) {
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(bodyBytes), nil
}

func extractYouTubeVideoID(pageURL string) string {
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	if strings.Contains(parsed.Host, "youtu.be") {
		id := strings.TrimPrefix(parsed.Path, "/")
		if idx := strings.Index(id, "/"); idx > 0 {
			id = id[:idx]
		}
		return id
	}
	if v := parsed.Query().Get("v"); v != "" {
		return v
	}
	if strings.HasPrefix(parsed.Path, "/shorts/") {
		return strings.TrimPrefix(parsed.Path, "/shorts/")
	}
	return ""
}

func proxyDownloadHandler(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		http.Error(w, "Missing url parameter", http.StatusBadRequest)
		return
	}

	if err := validateURL(rawURL); err != nil {
		http.Error(w, "Invalid URL: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := validateProxyURL(rawURL); err != nil {
		http.Error(w, "Forbidden: "+err.Error(), http.StatusForbidden)
		return
	}

	ytFormat := r.URL.Query().Get("yt_format")
	if ytFormat != "" {
		handleYtDlpDownload(w, r, rawURL, ytFormat)
		return
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		http.Error(w, "Invalid url parameter", http.StatusBadRequest)
		return
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	convertParam := strings.ToLower(r.URL.Query().Get("convert"))
	shouldConvertToMp3 := convertParam == "mp3"
	audioURL := r.URL.Query().Get("audio_url")

	// Forward Range header only when NOT converting/merging (merge needs full stream)
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" && !shouldConvertToMp3 && audioURL == "" {
		req.Header.Set("Range", rangeHeader)
	}

	// Set correct Referer per CDN origin
	host := strings.ToLower(parsed.Host)
	var referer string
	var extraHeaders map[string]string
	switch {
	case strings.Contains(host, "tiktokcdn") || strings.Contains(host, "tiktok-obj") ||
		strings.Contains(host, "tiktokv.com") || strings.Contains(host, "tiktokcdn-us"):
		referer = "https://www.tiktok.com/"
	case strings.Contains(host, "downloadgram") || strings.Contains(host, "cdn.downloadgram"):
		referer = "https://downloadgram.org/"
	case strings.Contains(host, "cdninstagram") || strings.Contains(host, "scontent") || strings.Contains(host, "fbcdn"):
		referer = "https://www.instagram.com/"
		extraHeaders = map[string]string{
			"Origin": "https://www.instagram.com",
			"Accept": "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8",
			"Sec-Fetch-Site": "cross-site",
			"Sec-Fetch-Mode": "no-cors",
			"Sec-Fetch-Dest": "image",
		}
	case strings.Contains(host, "googlevideo") || strings.Contains(host, "youtube"):
		referer = "https://www.youtube.com/"
	default:
		referer = parsed.Scheme + "://" + parsed.Host + "/"
	}
	req.Header.Set("Referer", referer)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
		// Don't follow redirects automatically — pass them through
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Re-set Referer on redirect
			req.Header.Set("Referer", referer)
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to fetch remote file", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || (resp.StatusCode >= 300 && resp.StatusCode != 206) {
		http.Error(w, fmt.Sprintf("Remote server returned %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(strings.ToLower(contentType), "text/html") {
		http.Error(w, "Remote URL returned HTML instead of media", http.StatusBadGateway)
		return
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Cache-Control", "no-cache")

	rawExt := strings.ToLower(strings.TrimPrefix(path.Ext(parsed.Path), "."))
	shouldConvertToMp3 = shouldConvertToMp3 && rawExt != "mp3" && !strings.Contains(strings.ToLower(contentType), "mp3")

	isPreview := r.URL.Query().Get("preview") == "1"
	shouldMergeAudio := audioURL != "" && !shouldConvertToMp3 && !isPreview

	if shouldMergeAudio {
		ffmpegPath := findFFmpeg()
		if _, err := os.Stat(ffmpegPath); err != nil {
			log.Printf("[proxy] ffmpeg not found at %s, skipping merge", ffmpegPath)
		} else {
			// Download audio to temp file (audio is small, typically <5MB)
			audioReq, err := http.NewRequest("GET", audioURL, nil)
			if err == nil {
				audioReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
				audioReq.Header.Set("Referer", "https://www.youtube.com/")
				if audioResp, err := client.Do(audioReq); err == nil {
					audioTmp, err := os.CreateTemp("", "amerge-*")
					if err == nil {
						tmpName := audioTmp.Name()
						written, _ := io.Copy(audioTmp, audioResp.Body)
						audioTmp.Close()
						audioResp.Body.Close()
						if written > 0 {
							w.Header().Set("Content-Type", "video/mp4")
							w.Header().Set("Content-Disposition", "attachment; filename=download.mp4")
							w.Header().Set("Accept-Ranges", "bytes")

							cmd := exec.CommandContext(r.Context(), ffmpegPath,
								"-hide_banner",
								"-loglevel", "error",
								"-i", "pipe:0",
								"-i", tmpName,
								"-c:v", "copy",
								"-c:a", "aac",
								"-map", "0:v:0",
								"-map", "1:a:0",
								"-movflags", "+faststart",
								"-f", "mp4",
								"pipe:1",
							)
							cmd.Stdin = resp.Body
							cmd.Stdout = w
							var stderr bytes.Buffer
							cmd.Stderr = &stderr
							if err := cmd.Run(); err != nil {
								log.Printf("[proxy] ffmpeg merge failed: %v | %s", err, stderr.String())
							}
							os.Remove(tmpName)
							return
						}
						os.Remove(tmpName)
					}
					audioResp.Body.Close()
				}
			}
			log.Printf("[proxy] audio merge fallback: serving video without merge")
		}
	}

	if shouldConvertToMp3 {
		ffmpegPath := findFFmpeg()
		if _, err := os.Stat(ffmpegPath); err == nil {
			w.Header().Set("Content-Type", "audio/mpeg")
			if r.URL.Query().Get("preview") == "1" {
				w.Header().Set("Content-Disposition", "inline; filename=preview.mp3")
			} else {
				w.Header().Set("Content-Disposition", "attachment; filename=download.mp3")
			}

			cmd := exec.CommandContext(r.Context(), ffmpegPath,
				"-hide_banner",
				"-loglevel", "error",
				"-i", "pipe:0",
				"-f", "mp3",
				"-codec:a", "libmp3lame",
				"-q:a", "2",
				"pipe:1",
			)
			cmd.Stdin = resp.Body
			cmd.Stdout = w
			var stderr bytes.Buffer
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				log.Printf("[proxy] ffmpeg conversion failed: %v | %s", err, stderr.String())
			}
			return
		}
		log.Printf("[proxy] ffmpeg not found at %s, serving audio in native format", ffmpegPath)
		shouldConvertToMp3 = false
	}

	w.Header().Set("Content-Type", contentType)
	// Forward Content-Length and Content-Range for video seeking support
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		w.Header().Set("Content-Range", cr)
	}
	// Accept-Ranges tells the browser it can seek
	w.Header().Set("Accept-Ranges", "bytes")

	fileExt := strings.ToLower(strings.TrimPrefix(path.Ext(parsed.Path), "."))
	if shouldConvertToMp3 {
		fileExt = "mp3"
	}
	if fileExt == "" {
		if strings.Contains(contentType, "jpeg") {
			fileExt = "jpg"
		} else if strings.Contains(contentType, "png") {
			fileExt = "png"
		} else if strings.Contains(contentType, "webp") {
			fileExt = "webp"
		} else if strings.Contains(contentType, "mp4") {
			fileExt = "mp4"
		} else if strings.Contains(contentType, "mpeg") || strings.Contains(contentType, "mp3") {
			fileExt = "mp3"
		} else if strings.Contains(contentType, "m4a") || strings.Contains(contentType, "mp4a") {
			fileExt = "m4a"
		} else {
			fileExt = "bin"
		}
	}
	disposition := "attachment"
	filename := r.URL.Query().Get("filename")
	if filename == "" {
		filename = fmt.Sprintf("download.%s", fileExt)
	} else if !strings.HasSuffix(filename, "."+fileExt) {
		filename += "." + fileExt
	}
	if r.URL.Query().Get("preview") == "1" {
		disposition = "inline"
		filename = fmt.Sprintf("preview.%s", fileExt)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, filename))

	// Write correct status code (206 for partial content / range requests)
	if resp.StatusCode == 206 {
		w.WriteHeader(http.StatusPartialContent)
	}

	io.Copy(w, resp.Body)
}

func isVideoExt(ext string) bool {
	ext = strings.ToLower(ext)
	return ext == "mp4" || ext == "webm" || ext == "mkv" || ext == "mov" || ext == "avi" || ext == "3gp" || ext == "flv"
}

func isImageExt(ext string) bool {
	ext = strings.ToLower(ext)
	return ext == "jpg" || ext == "jpeg" || ext == "png" || ext == "webp" || ext == "gif" || ext == "bmp"
}

func isLikelyImageCDN(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "scontent") ||
		strings.Contains(lower, "cdninstagram") ||
		strings.Contains(lower, "fbcdn") ||
		strings.Contains(lower, "tiktokcdn") ||
		strings.Contains(lower, "tiktokcdn-us") ||
		strings.Contains(lower, "p16-common-sign") ||
		strings.Contains(lower, "p19-common-sign") ||
		strings.Contains(lower, "pbs.twimg.com") ||
		strings.Contains(lower, "downloadgram")
}

func resolveImageExt(rawURL, ext string) string {
	if isImageExt(ext) {
		return ext
	}
	if e := detectExtFromURL(rawURL, ""); isImageExt(e) {
		return e
	}
	if isLikelyImageCDN(rawURL) {
		return "jpg"
	}
	return ""
}

func isAudioExt(ext string) bool {
	ext = strings.ToLower(ext)
	return ext == "mp3" || ext == "m4a" || ext == "aac" || ext == "wav" || ext == "opus"
}

func handleYtDlpDownload(w http.ResponseWriter, r *http.Request, pageURL, ytFormat string) {
	if !ytDlpAvailable() {
		http.Error(w, "yt-dlp not available on server", http.StatusServiceUnavailable)
		return
	}

	ffmpegPath := findFFmpeg()

	filename := r.URL.Query().Get("filename")
	if filename == "" {
		filename = "download"
	}

	isAudioOnly := strings.HasPrefix(ytFormat, "bestaudio") || strings.Contains(ytFormat, "mp3")
	needsMerge := strings.Contains(ytFormat, "+")
	setContentDisposition := func(ext string) {
		if !strings.HasSuffix(filename, "."+ext) {
			filename += "." + ext
		}
		if isAudioOnly {
			w.Header().Set("Content-Type", "audio/mpeg")
		} else {
			w.Header().Set("Content-Type", "video/mp4")
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	}

	args := []string{
		"--quiet",
		"--no-warnings",
		"--no-progress",
		"--js-runtimes", "node",
		"--remote-components", "ejs:github",
		"-f", ytFormat,
	}
	if needsMerge {
		tmpPath := filepath.Join(os.TempDir(), "ytdl-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".mp4")
		go func() { time.Sleep(10 * time.Second); os.Remove(tmpPath) }()

		args = append(args, "--merge-output-format", "mp4", "-o", tmpPath)
		if _, err := os.Stat(ffmpegPath); err == nil {
			args = append(args, "--ffmpeg-location", ffmpegPath)
		}
		args = append(args, pageURL)

		cmd := ytDlpCommand(args...)
		var stderrBuf bytes.Buffer
		cmd.Stderr = &stderrBuf
		if err := cmd.Run(); err != nil {
			log.Printf("[ytdlp] download failed: %v, stderr: %s", err, stderrBuf.String())
			http.Error(w, "Download failed", http.StatusInternalServerError)
			return
		}

		if _, err := os.Stat(tmpPath); err != nil || os.IsNotExist(err) {
			log.Printf("[ytdlp] merged file not found at %s", tmpPath)
			http.Error(w, "Download failed: merged file not found", http.StatusInternalServerError)
			return
		}

		if isAudioOnly {
			setContentDisposition("mp3")
		} else {
			setContentDisposition("mp4")
		}

		http.ServeFile(w, r, tmpPath)
		return
	}

	// Single format: pipe directly
	args = append(args, "-o", "-")
	if _, err := os.Stat(ffmpegPath); err == nil {
		args = append(args, "--ffmpeg-location", ffmpegPath)
	}
	args = append(args, pageURL)

	if isAudioOnly {
		setContentDisposition("mp3")
	} else {
		setContentDisposition("mp4")
	}

	cmd := ytDlpCommand(args...)
	cmd.Stdout = w
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		log.Printf("[ytdlp] download failed: %v, stderr: %s", err, stderrBuf.String())
	}
}

type ytOEmbed struct {
	Title        string `json:"title"`
	ThumbnailURL string `json:"thumbnail_url"`
}

func fetchYouTubeOEmbed(pageURL string) (title, thumbnail string) {
	videoID := extractYouTubeVideoID(pageURL)
	if videoID == "" {
		return "", ""
	}
	apiURL := fmt.Sprintf("https://www.youtube.com/oembed?url=https://www.youtube.com/watch?v=%s&format=json", videoID)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	var data ytOEmbed
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		return "", ""
	}
	return data.Title, data.ThumbnailURL
}

func isDirectImageURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	if isImageExt(strings.TrimPrefix(path.Ext(parsedPath(rawURL)), ".")) {
		return true
	}
	return strings.Contains(lower, ".jpg") || strings.Contains(lower, ".jpeg") ||
		strings.Contains(lower, ".png") || strings.Contains(lower, ".webp") ||
		strings.Contains(lower, "scontent") && (strings.Contains(lower, "jpg") || strings.Contains(lower, "webp"))
}

func parsedPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Path
}

func detectPlatform(pageURL string) string {
	if strings.Contains(pageURL, "instagram.com") {
		return "Instagram"
	}
	if strings.Contains(pageURL, "tiktok.com") || strings.Contains(pageURL, "vm.tiktok.com") || strings.Contains(pageURL, "vt.tiktok.com") {
		return "TikTok"
	}
	if strings.Contains(pageURL, "facebook.com") || strings.Contains(pageURL, "fb.watch") || strings.Contains(pageURL, "fb.com") {
		return "Facebook"
	}
	if strings.Contains(pageURL, "youtube.com") || strings.Contains(pageURL, "youtu.be") || strings.Contains(pageURL, "youtube-nocookie.com") {
		return "YouTube"
	}
	return "Unknown"
}

func cleanPageURL(pageURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil {
		return pageURL
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	path := strings.TrimSuffix(parsed.Path, "/")
	if path == "" {
		path = "/"
	}
	parsed.Path = path
	return parsed.String()
}

func normalizePageURL(pageURL, platform string) []string {
	clean := cleanPageURL(pageURL)
	urls := []string{clean, pageURL}
	if platform == "Instagram" {
		if strings.Contains(clean, "/p/") || strings.Contains(clean, "/reel/") {
			base := strings.TrimSuffix(clean, "/")
			if !strings.HasSuffix(base, "/embed") {
				urls = append(urls, base+"/embed/")
			}
		}
	}
	seen := make(map[string]bool)
	var unique []string
	for _, u := range urls {
		if u != "" && !seen[u] {
			seen[u] = true
			unique = append(unique, u)
		}
	}
	return unique
}

func fetchDownloadgramImages(pageURL string) (title string, options []MediaOption) {
	form := url.Values{"url": {cleanPageURL(pageURL)}}
	req, err := http.NewRequest("POST", "https://api.downloadgram.org/media", strings.NewReader(form.Encode()))
	if err != nil {
		log.Printf("[Downloadgram] request creation error: %v\n", err)
		return "", nil
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	req.Header.Set("Origin", "https://downloadgram.org")
	req.Header.Set("Referer", "https://downloadgram.org/")

	client := &http.Client{Timeout: 25 * time.Second}

	doRequest := func() (*http.Response, []byte, error) {
		r, e := client.Do(req)
		if e != nil {
			return nil, nil, e
		}
		defer r.Body.Close()
		b, e := io.ReadAll(r.Body)
		return r, b, e
	}

	resp, body, err := doRequest()
	if err != nil {
		log.Printf("[Downloadgram] request error: %v\n", err)
		return "", nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyStr := string(body)
		if len(bodyStr) > 500 { bodyStr = bodyStr[:500] }
		log.Printf("[Downloadgram] HTTP %d, %d bytes (will retry after 2s): %s\n", resp.StatusCode, len(body), bodyStr)
		time.Sleep(2 * time.Second)
		// Retry once
		resp, body, err = doRequest()
		if err != nil {
			log.Printf("[Downloadgram] retry error: %v\n", err)
			return "", nil
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			bodyStr := string(body)
			if len(bodyStr) > 500 { bodyStr = bodyStr[:500] }
			log.Printf("[Downloadgram] retry HTTP %d, %d bytes: %s\n", resp.StatusCode, len(body), bodyStr)
			return "", nil
		}
	}

	html := string(body)
	if t := extractFirstMeta(html, "og:title"); t != "" {
		title = t
	}

	seenFilename := make(map[string]bool)

	// Decode JWT tokens from downloadgram to extract real CDN URLs
	reToken := regexp.MustCompile(`https://cdn\.downloadgram\.org/\?token=([A-Za-z0-9._\-]+)`)
	tokenURLs := reToken.FindAllStringSubmatch(html, -1)
	log.Printf("[Downloadgram] Token URLs found: %d\n", len(tokenURLs))

	for _, m := range tokenURLs {
		if len(m) < 2 {
			continue
		}
		fullURL := m[0]
		filename, cdnURL := decodeDownloadgramToken(fullURL)
		if cdnURL != "" {
			dedupKey := "cdn:" + cdnURL
			if seenFilename[dedupKey] {
				log.Printf("[Downloadgram] Duplicate CDN URL\n")
				continue
			}
			seenFilename[dedupKey] = true
			ext := detectExtFromURL(cdnURL, "jpg")
			options = append(options, MediaOption{
				Quality: fmt.Sprintf("Foto %d - Resolusi Penuh", len(options)+1),
				Format:  ext,
				Size:    "Dinamis",
				URL:     cdnURL,
			})
			log.Printf("[Downloadgram] Using CDN URL from JWT: ext=%s, filename=%s\n", ext, filename)
		} else {
			// Fallback: use downloadgram proxy URL
			key := normalizeURLForDedup(fullURL)
			if seenFilename[key] {
				continue
			}
			seenFilename[key] = true
			options = append(options, MediaOption{
				Quality: fmt.Sprintf("Foto %d - Resolusi Penuh", len(options)+1),
				Format:  "jpg",
				Size:    "Dinamis",
				URL:     fullURL,
			})
		}
	}

	if title == "" && len(options) > 0 {
		title = "Instagram Photo"
	}
	filtered := filterImageOptions(options)
	log.Printf("[Downloadgram] After filter: %d options\n", len(filtered))
	return title, filtered
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func formatFilesize(bytes float64) string {
	if bytes <= 0 {
		return "Dinamis"
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.0f KB", bytes/1024)
	}
	return fmt.Sprintf("%.1f MB", bytes/(1024*1024))
}

func normalizeURLForDedup(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	// Untuk CDN Instagram/TikTok, path sudah cukup unik — strip query params
	if strings.Contains(parsed.Host, "scontent") ||
		strings.Contains(parsed.Host, "cdninstagram") ||
		strings.Contains(parsed.Host, "fbcdn") ||
		strings.Contains(parsed.Host, "tiktokcdn") {
		// Strip query params and also remove common Instagram CDN parameters from path
		p := parsed.Path
		// Remove /vp/ segments that indicate different versions
		p = regexp.MustCompile(`/vp/\d+/\d+/\d+`).ReplaceAllString(p, "")
		// Remove /t/ segments that indicate transformations
		p = regexp.MustCompile(`/t/\d+/\d+/\d+`).ReplaceAllString(p, "")
		return parsed.Scheme + "://" + parsed.Host + p
	}
	// Untuk cdn.downloadgram.org token URLs, ekstrak nama file dari token
	// Token biasanya mengandung nama file asli yang bisa dipakai sebagai dedup key
	if strings.Contains(parsed.Host, "downloadgram") {
		token := parsed.Query().Get("token")
		if token != "" {
			// Token adalah JWT — decode payload (bagian tengah) untuk ambil filename
			parts := strings.Split(token, ".")
			if len(parts) >= 2 {
				payload := parts[1]
				// Tambah padding base64 jika perlu
				for len(payload)%4 != 0 {
					payload += "="
				}
				decoded, err := base64.URLEncoding.DecodeString(payload)
				if err != nil {
					decoded, _ = base64.StdEncoding.DecodeString(payload)
				}
				if len(decoded) > 0 {
					var jwtPayload struct {
						Filename string `json:"filename"`
					}
					if json.Unmarshal(decoded, &jwtPayload) == nil && jwtPayload.Filename != "" {
						return "downloadgram:filename:" + jwtPayload.Filename
					}
				}
				// Fallback: gunakan payload mentah sebagai key
				return "downloadgram:token:" + parts[1]
			}
		}
	}
	return rawURL
}

func filterImageOptions(options []MediaOption) []MediaOption {
	var filtered []MediaOption
	seen := make(map[string]bool)
	seenFull := make(map[string]bool)
	for _, opt := range options {
		if opt.URL == "" || isVideoExt(opt.Format) || isAudioExt(opt.Format) {
			continue
		}
		// Resolve extension — if Format is already a valid image ext, use it directly
		ext := opt.Format
		if !isImageExt(ext) {
			if resolved := resolveImageExt(opt.URL, opt.Format); resolved != "" {
				ext = resolved
			} else {
				log.Printf("[Filter] Skipping non-image URL: %s\n", opt.URL)
				continue
			}
		}
		opt.Format = ext
		dedupKey := normalizeURLForDedup(opt.URL)
		if seen[dedupKey] || seenFull[opt.URL] {
			log.Printf("[Filter] Skipping duplicate: %s\n", dedupKey)
			continue
		}
		seen[dedupKey] = true
		seenFull[opt.URL] = true
		filtered = append(filtered, opt)
	}
	log.Printf("[Filter] Dedup result: %d -> %d\n", len(options), len(filtered))
	return filtered
}

func filterVideoOptions(options []MediaOption) []MediaOption {
	var filtered []MediaOption
	seen := make(map[string]bool)
	for _, opt := range options {
		if opt.URL == "" {
			continue
		}
		// Always check actual URL extension — even if Format claims "mp4",
		// the URL might still point to an image (e.g. query param contains ".mp4").
		ext := detectExtFromURL(opt.URL, opt.Format)
		if isImageExt(ext) {
			continue
		}
		if !isVideoExt(ext) && !isAudioExt(ext) && ext != "" && ext != opt.Format {
			continue
		}
		if seen[opt.URL] {
			continue
		}
		seen[opt.URL] = true
		filtered = append(filtered, opt)
	}
	return filtered
}

func completeSSE(w http.ResponseWriter, flusher http.Flusher, mediaType, title, duration, thumbnail string, options []MediaOption) {
	if len(options) == 0 {
		return
	}
	if thumbnail == "" {
		thumbnail = options[0].URL
	}
	sendSSE(w, EventMessage{Status: "Menyelesaikan...", Progress: 95, MediaType: mediaType})
	flusher.Flush()
	time.Sleep(200 * time.Millisecond)
	sendSSE(w, EventMessage{
		Status:    "Completed",
		Progress:  100,
		MediaType: mediaType,
		Title:     title,
		Duration:  duration,
		Thumbnail: thumbnail,
		Options:   options,
		MediaURL:  options[0].URL,
	})
	flusher.Flush()
}

func buildImageOptionsFromHTML(html, platform, pageURL string) (title, thumbnail string, options []MediaOption) {
	title = extractFirstMeta(html, "og:title", "twitter:title")
	if title == "" {
		title = fmt.Sprintf("%s Photo", platform)
	}
	thumbnail = extractFirstMeta(html, "og:image:secure_url", "og:image", "twitter:image")
	embeddedImages := extractEmbeddedImageURLs(html)

	seenURLs := make(map[string]bool)
	addOption := func(quality, mediaURL string) {
		if mediaURL == "" {
			return
		}
		dedupKey := normalizeURLForDedup(mediaURL)
		if seenURLs[dedupKey] {
			return
		}
		ext := resolveImageExt(mediaURL, "")
		if ext == "" || isVideoExt(ext) {
			return
		}
		seenURLs[dedupKey] = true
		options = append(options, MediaOption{
			Quality: quality,
			Format:  ext,
			Size:    "Dinamis",
			URL:     mediaURL,
		})
	}

	for i, imgURL := range embeddedImages {
		label := "Foto Resolusi Penuh"
		if len(embeddedImages) > 1 {
			label = fmt.Sprintf("Foto %d / %d - Resolusi Penuh", i+1, len(embeddedImages))
		}
		addOption(label, imgURL)
	}

	if thumbnail != "" {
		if len(options) == 0 {
			addOption("Foto Resolusi Penuh (OG)", thumbnail)
		} else if !seenURLs[thumbnail] {
			addOption("Foto Cover / Thumbnail", thumbnail)
		}
	}

	if platform == "YouTube" && len(options) == 0 {
		if vid := extractYouTubeVideoID(pageURL); vid != "" {
			maxThumb := fmt.Sprintf("https://img.youtube.com/vi/%s/maxresdefault.jpg", vid)
			addOption("Thumbnail Max Resolution", maxThumb)
			thumbnail = maxThumb
		}
	}

	return title, thumbnail, options
}

func runImageDownload(w http.ResponseWriter, flusher http.Flusher, pageURL, platform string) {
	cleanURL := cleanPageURL(pageURL)
	log.Printf("[Image] Memproses link foto %s: %s\n", platform, cleanURL)

	// Direct image URL
	if isDirectImageURL(pageURL) || isDirectImageURL(cleanURL) {
		target := cleanURL
		if isDirectImageURL(pageURL) {
			target = pageURL
		}
		ext := detectExtFromURL(target, "jpg")
		opts := []MediaOption{{Quality: "Foto Langsung", Format: ext, Size: "Dinamis", URL: target}}
		completeSSE(w, flusher, "image", "Direct Image", "0:00", target, opts)
		return
	}

	var title, thumbnail string
	var options []MediaOption

	switch platform {
	case "Instagram":
		sendSSE(w, EventMessage{Status: "Mengekstrak foto dari Instagram...", Progress: 20, MediaType: "image"})
		flusher.Flush()
		title, thumbnail, options = fetchInstagramImages(cleanURL)
		log.Printf("[Image] Instagram engine: %d foto\n", len(options))

	case "TikTok":
		sendSSE(w, EventMessage{Status: "Mengekstrak foto dari TikTok...", Progress: 20, MediaType: "image"})
		flusher.Flush()
		title, thumbnail, options = fetchTikTokImages(cleanURL)
		log.Printf("[Image] TikTok engine: %d foto\n", len(options))

	case "Facebook":
		sendSSE(w, EventMessage{Status: "Mengekstrak foto dari Facebook...", Progress: 20, MediaType: "image"})
		flusher.Flush()
		// Facebook: coba HTML scraping dulu
		for _, tryURL := range normalizePageURL(cleanURL, platform) {
			html, err := fetchPageHTML(tryURL, platformReferer(platform))
			if err != nil {
				continue
			}
			t, th, opts := buildImageOptionsFromHTML(html, platform, cleanURL)
			opts = filterImageOptions(opts)
			if len(opts) > len(options) {
				title, thumbnail, options = t, th, opts
			}
		}
		log.Printf("[Image] Facebook HTML scrape: %d foto\n", len(options))

	case "YouTube":
		sendSSE(w, EventMessage{Status: "Mengekstrak thumbnail YouTube...", Progress: 20, MediaType: "image"})
		flusher.Flush()
		if vid := extractYouTubeVideoID(cleanURL); vid != "" {
			// YouTube: ambil semua resolusi thumbnail
			thumbURLs := []struct{ url, label string }{
				{fmt.Sprintf("https://img.youtube.com/vi/%s/maxresdefault.jpg", vid), "Thumbnail Max Resolution (1280x720)"},
				{fmt.Sprintf("https://img.youtube.com/vi/%s/sddefault.jpg", vid), "Thumbnail SD (640x480)"},
				{fmt.Sprintf("https://img.youtube.com/vi/%s/hqdefault.jpg", vid), "Thumbnail HQ (480x360)"},
				{fmt.Sprintf("https://img.youtube.com/vi/%s/mqdefault.jpg", vid), "Thumbnail MQ (320x180)"},
			}
			title = "YouTube Thumbnail"
			for _, t := range thumbURLs {
				if thumbnail == "" {
					thumbnail = t.url
				}
				options = append(options, MediaOption{
					Quality: t.label,
					Format:  "jpg",
					Size:    "Dinamis",
					URL:     t.url,
				})
			}
		}
		log.Printf("[Image] YouTube thumbnails: %d\n", len(options))
	}

	if len(options) > 0 {
		log.Printf("[Image] Final: %d foto dikirim ke frontend\n", len(options))
		completeSSE(w, flusher, "image", title, "0:00", thumbnail, options)
		return
	}

	sendSSE(w, EventMessage{
		Status: "Error",
		Error:  "Tidak dapat mengekstrak foto. Pastikan link adalah post foto publik yang dapat dibuka tanpa login.",
	})
	flusher.Flush()
}

func runRealDownload(w http.ResponseWriter, flusher http.Flusher, pageURL, platform string) {
	sendSSE(w, EventMessage{Status: fmt.Sprintf("Menganalisis link %s dengan custom engine...", platform), Progress: 40})
	flusher.Flush()

	log.Printf("[Downloader] Menjalankan custom engine untuk link %s...\n", pageURL)

	// Try custom video engines first
	title, duration, thumbnail, options := fetchVideos(pageURL, platform)

	if len(options) > 0 {
		sendSSE(w, EventMessage{Status: "Menyelesaikan...", Progress: 95})
		flusher.Flush()
		time.Sleep(200 * time.Millisecond)
		completeSSE(w, flusher, "video", title, duration, thumbnail, options)
		return
	}

	if platform != "YouTube" && runOgVideoFallback(w, flusher, pageURL, platform) {
		return
	}

	sendSSE(w, EventMessage{Status: "Menyelesaikan...", Progress: 95})
	flusher.Flush()
	time.Sleep(200 * time.Millisecond)
	sendSSE(w, EventMessage{
		Status: "Error",
		Error:  fmt.Sprintf("Tidak dapat mengekstrak video dari %s. Pastikan link benar dan video dapat diakses publik.", platform),
	})
	flusher.Flush()
}

func runOgVideoFallback(w http.ResponseWriter, flusher http.Flusher, pageURL, platform string) bool {
	html, err := fetchPageHTML(pageURL, platformReferer(platform))
	if err != nil {
		return false
	}
	videoURL := extractFirstMeta(html, "og:video:secure_url", "og:video", "og:video:url")
	if videoURL == "" {
		return false
	}
	title := extractFirstMeta(html, "og:title", "twitter:title")
	if title == "" {
		title = fmt.Sprintf("%s Video", platform)
	}
	thumbnail := extractFirstMeta(html, "og:image:secure_url", "og:image", "twitter:image")
	opts := []MediaOption{{
		Quality: "Video Resolusi Penuh",
		Format:  detectExtFromURL(videoURL, "mp4"),
		Size:    "Dinamis",
		URL:     videoURL,
	}}
	completeSSE(w, flusher, "video", title, "0:00", thumbnail, opts)
	return true
}



func setupSSE(w http.ResponseWriter) (http.Flusher, bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	return flusher, ok
}

func validateStreamRequest(w http.ResponseWriter, flusher http.Flusher, pageURL string) (string, bool) {
	if err := validateURL(pageURL); err != nil {
		sendSSE(w, EventMessage{Status: "Error", Error: "URL tidak valid: " + err.Error()})
		flusher.Flush()
		return "", false
	}
	platform := detectPlatform(pageURL)
	if platform == "Unknown" {
		sendSSE(w, EventMessage{Status: "Error", Error: "Platform tidak didukung. Harap masukkan link YouTube, IG, TikTok, atau FB."})
		flusher.Flush()
		return "", false
	}
	return platform, true
}

func videoDownloadHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := setupSSE(w)
	if !ok {
		http.Error(w, "Streaming tidak didukung!", http.StatusInternalServerError)
		return
	}

	pageURL := r.URL.Query().Get("url")
	platform, valid := validateStreamRequest(w, flusher, pageURL)
	if !valid {
		return
	}

	log.Printf("[Video] Request %s: %s\n", platform, pageURL)
	runRealDownload(w, flusher, pageURL, platform)
}

func imageDownloadHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := setupSSE(w)
	if !ok {
		http.Error(w, "Streaming tidak didukung!", http.StatusInternalServerError)
		return
	}

	pageURL := r.URL.Query().Get("url")
	platform, valid := validateStreamRequest(w, flusher, pageURL)
	if !valid {
		return
	}

	log.Printf("[Image] Request %s: %s\n", platform, pageURL)
	runImageDownload(w, flusher, pageURL, platform)
}

// Legacy endpoint — default ke video
func downloadHandler(w http.ResponseWriter, r *http.Request) {
	// Strip "/api/stream/" prefix to get source type
	source := strings.TrimPrefix(r.URL.Path, "/api/stream/")
	r.URL.Path = "/api/stream"
	q := r.URL.Query()
	q.Set("source", source)
	r.URL.RawQuery = q.Encode()
	videoDownloadHandler(w, r)
}

func sendSSE(w http.ResponseWriter, msg EventMessage) {
	data, _ := json.Marshal(msg)
	fmt.Fprintf(w, "data: %s\n\n", string(data))
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/stream/", downloadHandler)
	mux.HandleFunc("/api/stream/video", videoDownloadHandler)
	mux.HandleFunc("/api/stream/image", imageDownloadHandler)
	mux.HandleFunc("/api/proxy", proxyDownloadHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	fmt.Println("Backend realtime server berjalan di port :" + port)

	handler := loggingMiddleware(
		recoveryMiddleware(
			rateLimitMiddleware(
				securityHeadersMiddleware(
					corsMiddleware(mux),
				),
			),
		),
	)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}
