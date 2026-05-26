package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	yt "github.com/kkdai/youtube/v2"
)

func fetchVideos(pageURL, platform string) (title string, duration string, thumbnail string, options []MediaOption) {
	switch platform {
	case "YouTube":
		return fetchYouTubeVideos(pageURL, platform)
	case "Instagram":
		return fetchInstagramVideos(pageURL)
	case "TikTok":
		return fetchTikTokVideos(pageURL)
	case "Facebook":
		return fetchFacebookVideos(pageURL)
	default:
		return "", "", "", nil
	}
}

func fetchYouTubeThumbnailsOnly(videoID string) (title string, duration string, thumbnail string, options []MediaOption) {
	title = "YouTube Video"
	thumbnail = fmt.Sprintf("https://img.youtube.com/vi/%s/maxresdefault.jpg", videoID)
	ytPage := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
	options = append(options, MediaOption{
		Quality: "Thumbnail Max Resolution (1280x720)",
		Format:  "jpg",
		Size:    "Dinamis",
		URL:     ytPage,
	})
	return title, duration, thumbnail, options
}

var (
	ytDlpBinary     string
	ytDlpModuleArgs []string
	ytDlpInited     bool
)

func initYtDlp() {
	ytDlpInited = true
	if p, err := exec.LookPath("yt-dlp"); err == nil && p != "" {
		ytDlpBinary = p
		ytDlpModuleArgs = nil
		return
	}
	candidates := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Packages", "PythonSoftwareFoundation.Python.3.13_qbz5n2kfra8p0", "LocalCache", "local-packages", "Python313", "Scripts", "yt-dlp.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Packages", "yt-dlp.yt-dlp_Microsoft.Winget.Source_8wekyb3d8bbwe", "yt-dlp.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			ytDlpBinary = c
			ytDlpModuleArgs = nil
			return
		}
	}
	for _, py := range []string{"python3", "python"} {
		if p, err := exec.LookPath(py); err == nil && p != "" {
			cmd := exec.Command(p, "-m", "yt_dlp", "--version")
			if cmd.Run() == nil {
				ytDlpBinary = p
				ytDlpModuleArgs = []string{"-m", "yt_dlp"}
				return
			}
		}
	}
	ytDlpBinary = "yt-dlp"
	ytDlpModuleArgs = nil
}

func findYtDlp() string {
	if !ytDlpInited {
		initYtDlp()
	}
	return ytDlpBinary
}

func ytDlpCommand(args ...string) *exec.Cmd {
	if !ytDlpInited {
		initYtDlp()
	}
	fullArgs := append(ytDlpModuleArgs, args...)
	return exec.Command(ytDlpBinary, fullArgs...)
}

func ytDlpCommandContext(ctx context.Context, args ...string) *exec.Cmd {
	if !ytDlpInited {
		initYtDlp()
	}
	fullArgs := append(ytDlpModuleArgs, args...)
	return exec.CommandContext(ctx, ytDlpBinary, fullArgs...)
}

func ytDlpAvailable() bool {
	if !ytDlpInited {
		initYtDlp()
	}
	if ytDlpModuleArgs != nil {
		return true
	}
	if _, err := os.Stat(ytDlpBinary); err == nil {
		return true
	}
	return false
}

type ytDlpFormat struct {
	FormatID       string  `json:"format_id"`
	Ext            string  `json:"ext"`
	Height         int     `json:"height"`
	Width          int     `json:"width"`
	FPS            float64 `json:"fps"`
	VCodec         string  `json:"vcodec"`
	ACodec         string  `json:"acodec"`
	TBR            float64 `json:"tbr"`
	Filesize       float64 `json:"filesize"`
	FilesizeApprox float64 `json:"filesize_approx"`
	Protocol       string  `json:"protocol"`
}

type ytDlpInfo struct {
	Title       string         `json:"title"`
	Duration    float64        `json:"duration"`
	Thumbnail   string         `json:"thumbnail"`
	Formats     []ytDlpFormat  `json:"formats"`
}

type sortFormat struct {
	vf       ytDlpFormat
	hasAudio bool
}

func runYtDlp(pageURL string, extraArgs ...string) (ytDlpInfo, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stdout, stderrBuf bytes.Buffer
	args := []string{
		"-J", "--no-download",
		"--no-warnings",
	}
	args = append(args, cookieArgs()...)
	args = append(args, proxyArgs()...)
	args = append(args, extraArgs...)
	args = append(args, pageURL)
	cmd := ytDlpCommandContext(ctx, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		log.Printf("[yt-dlp] run failed: %v | stderr: %s\n", err, stderrBuf.String()[:min(200, stderrBuf.Len())])
		return ytDlpInfo{}, false
	}
	var info ytDlpInfo
	if json.Unmarshal(stdout.Bytes(), &info) != nil {
		log.Printf("[yt-dlp] JSON parse error, stderr: %s\n", stderrBuf.String()[:min(200, stderrBuf.Len())])
		return ytDlpInfo{}, false
	}
	return info, true
}

func fetchYouTubeVideos(pageURL, platform string) (title string, duration string, thumbnail string, options []MediaOption) {
	videoID := extractYouTubeVideoID(pageURL)
	if videoID == "" {
		log.Printf("[YouTube] Cannot extract video ID from: %s\n", pageURL)
		return "", "", "", nil
	}
	log.Printf("[YouTube] Video ID: %s\n", videoID)

	if !ytDlpAvailable() {
		log.Printf("[YouTube] yt-dlp not found, using kkdai fallback\n")
		return fetchYouTubeViaKkdai(pageURL, videoID)
	}

	// Try yt-dlp with top client configs — try a few and pick best result
	type clientResult struct {
		label   string
		title   string
		dur     string
		thumb   string
		options []MediaOption
	}
	var bestResult *clientResult
	clientAttempts := []struct {
		label string
		args  []string
	}{
		{"tv", []string{"--extractor-args", "youtube:player_client=tv,youtube:include_dash_manifest=False", "--retries", "3", "--extractor-retries", "3", "--js-runtimes", "deno", "--force-ipv4"}},
		{"android+skip", []string{"--extractor-args", "youtube:player_client=android,youtube:skip=webpage", "--retries", "3", "--extractor-retries", "3", "--js-runtimes", "deno", "--force-ipv4"}},
		{"web", []string{"--extractor-args", "youtube:player_client=web", "--retries", "3", "--extractor-retries", "3", "--js-runtimes", "deno", "--force-ipv4"}},
		{"default", []string{"--retries", "3", "--extractor-retries", "3", "--js-runtimes", "deno", "--force-ipv4"}},
	}
	for _, attempt := range clientAttempts {
		log.Printf("[YouTube] trying yt-dlp with %s client\n", attempt.label)
		var info ytDlpInfo
		var ok bool
		if attempt.args != nil {
			info, ok = runYtDlp(pageURL, attempt.args...)
		} else {
			info, ok = runYtDlp(pageURL)
		}
		if !ok {
			continue
		}
		t, d, th, opts := buildYouTubeOptionsFromInfo(info, videoID)
		if len(opts) == 0 {
			continue
		}
		log.Printf("[YouTube] %s client: %d options\n", attempt.label, len(opts))
		if bestResult == nil || len(opts) > len(bestResult.options) {
			bestResult = &clientResult{label: attempt.label, title: t, dur: d, thumb: th, options: opts}
		}
		// If we have a good result with at least 3 options + audio, stop early
		if len(opts) >= 4 {
			break
		}
	}
	if bestResult != nil {
		log.Printf("[YouTube] using %s client result: %d options\n", bestResult.label, len(bestResult.options))
		return bestResult.title, bestResult.dur, bestResult.thumb, bestResult.options
	}

	log.Printf("[YouTube] all yt-dlp clients failed, trying kkdai\n")
	if t, d, th, opts := fetchYouTubeViaKkdai(pageURL, videoID); len(opts) > 0 {
		return t, d, th, opts
	}

	log.Printf("[YouTube] kkdai failed, trying Piped API\n")
	if t, d, th, opts := fetchYouTubeViaPiped(videoID); len(opts) > 0 {
		return t, d, th, opts
	}

	log.Printf("[YouTube] Piped failed, trying Invidious API\n")
	if t, d, th, opts := fetchYouTubeViaInvidious(videoID); len(opts) > 0 {
		return t, d, th, opts
	}

	log.Printf("[YouTube] Invidious failed, trying direct format fetch\n")
	if t, d, th, opts := fetchYouTubeViaDirectPipe(pageURL, videoID); len(opts) > 0 {
		return t, d, th, opts
	}

	// All real extraction methods failed.
	return "", "", "", nil
}

func buildYouTubeOptions(info ytDlpInfo, videoID string) (title string, duration string, thumbnail string, options []MediaOption) {
	return buildYouTubeOptionsFromInfo(info, videoID)
}

func buildYouTubeOptionsFromInfo(info ytDlpInfo, videoID string) (title string, duration string, thumbnail string, options []MediaOption) {
	title = info.Title
	if title == "" {
		title = "YouTube Video"
	}
	thumbnail = info.Thumbnail
	if thumbnail == "" {
		thumbnail = fmt.Sprintf("https://img.youtube.com/vi/%s/maxresdefault.jpg", videoID)
	}
	if info.Duration > 0 {
		duration = fmt.Sprintf("%d:%02d", int(info.Duration)/60, int(info.Duration)%60)
	}
	if duration == "" {
		duration = "0:00"
	}

	ytPage := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)

	// Check if ffmpeg is available
	ffmpegAvailable := false
	if _, err := os.Stat(findFFmpeg()); err == nil {
		ffmpegAvailable = true
	}

	var sorted []sortFormat
	seenHeight := make(map[int]bool)
	for _, f := range info.Formats {
		if f.VCodec == "none" || f.Height <= 0 {
			continue
		}
		if f.Ext != "mp4" {
			continue
		}
		// Skip HLS formats — yt-dlp reports them as mp4
		// but they are actually MPEG-TS streams from HLS.
		// HLS protocols: m3u8, m3u8_native, m3u8_fragmented
		protocol := strings.ToLower(f.Protocol)
		if strings.Contains(protocol, "m3u8") || strings.Contains(protocol, "hls") {
			continue
		}
		// Also skip by format ID: 91-99 and 300-309 are known HLS ranges
		if fid, _ := strconv.Atoi(f.FormatID); (fid >= 91 && fid <= 99) || (fid >= 300 && fid <= 399) {
			continue
		}
		if seenHeight[f.Height] {
			continue
		}
		hasAudio := f.ACodec != "none" && f.ACodec != ""
		if !hasAudio && !ffmpegAvailable {
			continue
		}
		seenHeight[f.Height] = true
		sorted = append(sorted, sortFormat{vf: f, hasAudio: hasAudio})
	}

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].vf.Height != sorted[j].vf.Height {
			return sorted[i].vf.Height > sorted[j].vf.Height
		}
		if sorted[i].vf.FPS != sorted[j].vf.FPS {
			return sorted[i].vf.FPS > sorted[j].vf.FPS
		}
		return sorted[i].vf.TBR > sorted[j].vf.TBR
	})

	getFormatSize := func(f ytDlpFormat) string {
		if f.Filesize > 0 {
			return formatFilesize(f.Filesize)
		}
		if f.FilesizeApprox > 0 {
			return formatFilesize(f.FilesizeApprox)
		}
		if info.Duration > 0 && f.TBR > 0 {
			estimated := (f.TBR * 1000 / 8) * info.Duration
			if estimated > 0 {
				return "~" + formatFilesize(estimated)
			}
		}
		return "Dinamis"
	}

	var audioFormat ytDlpFormat
	for _, f := range info.Formats {
		if strings.HasPrefix(f.FormatID, "140") || (f.Ext == "m4a" && f.ACodec != "none" && f.VCodec == "none") {
			if f.Filesize > 0 || f.FilesizeApprox > 0 || f.TBR > 0 {
				audioFormat = f
			}
		}
	}

	for _, s := range sorted {
		v := s.vf
		label := fmt.Sprintf("%dp", v.Height)
		if v.FPS > 30 {
			label = fmt.Sprintf("%s %dfps", label, int(v.FPS))
		}
		formatStr := v.FormatID
		if !s.hasAudio {
			formatStr = v.FormatID + "+bestaudio[ext=m4a]"
			if audioFormat.Filesize > 0 || audioFormat.FilesizeApprox > 0 || audioFormat.TBR > 0 {
				audioSize := audioFormat.Filesize
				if audioSize <= 0 {
					audioSize = audioFormat.FilesizeApprox
				}
				if audioSize <= 0 && info.Duration > 0 && audioFormat.TBR > 0 {
					audioSize = (audioFormat.TBR * 1000 / 8) * info.Duration
				}
				totalSize := v.Filesize
				if totalSize <= 0 {
					totalSize = v.FilesizeApprox
				}
				if totalSize <= 0 && info.Duration > 0 && v.TBR > 0 {
					totalSize = (v.TBR * 1000 / 8) * info.Duration
				}
				if totalSize > 0 && audioSize > 0 {
					options = append(options, MediaOption{
						Quality:  fmt.Sprintf("Video %s (MP4)", label),
						Format:   "mp4",
						Size:     "~" + formatFilesize(totalSize+audioSize),
						URL:      ytPage,
						YtFormat: formatStr,
					})
					continue
				}
			}
		}
		options = append(options, MediaOption{
			Quality:  fmt.Sprintf("Video %s (MP4)", label),
			Format:   "mp4",
			Size:     getFormatSize(v),
			URL:      ytPage,
			YtFormat: formatStr,
		})
	}

	options = append(options, MediaOption{
		Quality:  "Audio Only (MP3)",
		Format:   "mp3",
		Size:     getFormatSize(audioFormat),
		URL:      ytPage,
		YtFormat: "bestaudio[ext=m4a]",
	})

	if len(options) > 0 {
		log.Printf("[YouTube] %d format options\n", len(options))
	}
	return title, duration, thumbnail, options
}

func fetchYouTubeViaKkdai(pageURL, videoID string) (title string, duration string, thumbnail string, options []MediaOption) {
	client := yt.Client{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	video, err := client.GetVideoContext(ctx, pageURL)
	if err != nil {
		if strings.Contains(err.Error(), "TLS handshake timeout") || strings.Contains(err.Error(), "context deadline exceeded") {
			log.Printf("[YouTube] kkdai network error (HF Spaces may be blocking YouTube): %v\n", err)
		} else {
			log.Printf("[YouTube] kkdai error: %v\n", err)
		}
		return "", "", "", nil
	}

	title = video.Title
	if title == "" {
		title = "YouTube Video"
	}
	thumbnail = fmt.Sprintf("https://img.youtube.com/vi/%s/maxresdefault.jpg", videoID)
	dur := int(video.Duration.Seconds())
	duration = fmt.Sprintf("%d:%02d", dur/60, dur%60)

	ytPage := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)

	for _, f := range video.Formats {
		if f.URL == "" {
			continue
		}
		q := f.Quality
		if q == "" {
			q = fmt.Sprintf("itag-%d", f.ItagNo)
		}
		options = append(options, MediaOption{
			Quality: fmt.Sprintf("Video %s", q),
			Format:  "mp4",
			Size:    "Dinamis",
			URL:     f.URL,
		})
	}
	if len(options) == 0 {
		options = append(options, MediaOption{
			Quality:  "Video (Best Quality)",
			Format:   "mp4",
			Size:     "Dinamis",
			URL:      ytPage,
			YtFormat: "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]",
		})
	}
	return title, duration, thumbnail, options
}

// ─── Method 4: Invidious API ──────────────────────────────────────────────────

type invidiousFormat struct {
	URL  string `json:"url"`
	Type string `json:"type"`
	Quality string `json:"quality"`
	Bitrate int    `json:"bitrate"`
	Encoding string `json:"encoding"`
}

type invidiousVideo struct {
	Title       string             `json:"title"`
	VideoID     string             `json:"videoId"`
	Length      float64            `json:"lengthSeconds"`
	FormatStreams []invidiousFormat `json:"formatStreams"`
	AdaptiveFormats []invidiousFormat `json:"adaptiveFormats"`
	Author      string             `json:"author"`
	VideoThumbnails []struct {
		URL      string `json:"url"`
		Quality  string `json:"quality"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
	} `json:"videoThumbnails"`
}

func fetchYouTubeViaInvidious(videoID string) (title string, duration string, thumbnail string, options []MediaOption) {
	instances := []string{
		"https://invidious.projectsegfau.lt",
		"https://yewtu.be",
		"https://inv.nadeko.net",
		"https://invidious.slipfox.xyz",
		"https://vid.puffyan.us",
		"https://inv.odyssey346.dev",
		"https://invidious.privacydev.net",
	}
	for _, inst := range instances {
		apiURL := fmt.Sprintf("%s/api/v1/videos/%s", inst, videoID)
		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		var vid invidiousVideo
		if json.Unmarshal(body, &vid) != nil {
			continue
		}
		if vid.Title == "" {
			continue
		}
		title = vid.Title
		if vid.Length > 0 {
			duration = fmt.Sprintf("%d:%02d", int(vid.Length)/60, int(vid.Length)%60)
		}
		if len(vid.VideoThumbnails) > 0 {
			thumbnail = vid.VideoThumbnails[len(vid.VideoThumbnails)-1].URL
		}
		if thumbnail == "" {
			thumbnail = fmt.Sprintf("https://img.youtube.com/vi/%s/maxresdefault.jpg", videoID)
		}

		seen := make(map[string]bool)
		for _, f := range vid.FormatStreams {
			if f.URL == "" || seen[f.URL] {
				continue
			}
			seen[f.URL] = true
			q := strings.ToLower(f.Quality)
			isHD := strings.Contains(q, "hd") || strings.Contains(q, "1080") || strings.Contains(q, "720")
			isSD := strings.Contains(q, "480") || strings.Contains(q, "360")
			label := "Video"
			if isHD {
				label += " HD"
			} else if isSD {
				label += " SD"
			}
			if f.Quality != "" {
				label += " (" + f.Quality + ")"
			}
			options = append(options, MediaOption{
				Quality: label,
				Format:  detectExtFromURL(f.URL, "mp4"),
				Size:    "Dinamis",
				URL:     f.URL,
			})
		}
		for _, f := range vid.AdaptiveFormats {
			if f.URL == "" || seen[f.URL] {
				continue
			}
			seen[f.URL] = true
			isAudio := strings.Contains(f.Type, "audio")
			if isAudio {
				options = append(options, MediaOption{
					Quality: "Audio Only (Invidious)",
					Format:  "m4a",
					Size:    "Dinamis",
					URL:     f.URL,
				})
			} else {
				label := "Video Adaptive"
				if f.Quality != "" {
					label += " (" + f.Quality + ")"
				}
				options = append(options, MediaOption{
					Quality: label,
					Format:  detectExtFromURL(f.URL, "mp4"),
					Size:    "Dinamis",
					URL:     f.URL,
				})
			}
		}
		if len(options) > 0 {
			log.Printf("[YouTube] Invidious instance %s succeeded: %d options\n", inst, len(options))
			return title, duration, thumbnail, options
		}
	}
	return "", "", "", nil
}

// ─── Method 5: Direct yt-dlp pipe (format best) ──────────────────────────────

func fetchYouTubeViaDirectPipe(pageURL, videoID string) (title string, duration string, thumbnail string, options []MediaOption) {
	if !ytDlpAvailable() {
		return "", "", "", nil
	}

	// Get oEmbed metadata for title/thumbnail
	title, thumbnail = fetchYouTubeOEmbed(pageURL)
	if title == "" {
		title = "YouTube Video"
	}
	if thumbnail == "" {
		thumbnail = fmt.Sprintf("https://img.youtube.com/vi/%s/maxresdefault.jpg", videoID)
	}

	// Try to get a direct download URL with best format
	var stdout, stderrBuf bytes.Buffer
	args := []string{
		"-g", "-f", "best[ext=mp4]",
		"--no-download",
		"--retries", "3",
		"--js-runtimes", "deno",
		"--force-ipv4",
	}
	args = append(args, cookieArgs()...)
	args = append(args, proxyArgs()...)
	args = append(args, pageURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := ytDlpCommandContext(ctx, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderrBuf
	if cmd.Run() != nil {
		stderrStr := stderrBuf.String()
		if len(stderrStr) > 500 {
			stderrStr = stderrStr[:500]
		}
		log.Printf("[YouTube DirectPipe] yt-dlp failed: %v | stderr: %s\n", cmd.Err, stderrStr)
		return "", "", "", nil
	}

	directURL := strings.TrimSpace(stdout.String())
	if directURL == "" {
		return "", "", "", nil
	}

	options = append(options, MediaOption{
		Quality: "Video (Best Quality - Direct)",
		Format:  "mp4",
		Size:    "Dinamis",
		URL:     directURL,
	})
	options = append(options, MediaOption{
		Quality: "Audio Only (Best - Direct)",
		Format:  "m4a",
		Size:    "Dinamis",
		URL:     directURL,
		YtFormat: "bestaudio[ext=m4a]",
	})
	return title, duration, thumbnail, options
}

// ─── Method 6: Piped API ───────────────────────────────────────────────────────

type pipedStream struct {
	URL      string `json:"url"`
	Quality  string `json:"quality"`
	MimeType string `json:"mimeType"`
	Codec    string `json:"codec"`
	VideoURL string `json:"videoURL"`
}

type pipedVideo struct {
	Title         string        `json:"title"`
	VideoID       string        `json:"videoId"`
	Duration      int           `json:"duration"`
	ThumbnailURL  string        `json:"thumbnailUrl"`
	UploadDate    string        `json:"uploadDate"`
	VideoStreams  []pipedStream `json:"videoStreams"`
	AudioStreams  []pipedStream `json:"audioStreams"`
}

func fetchYouTubeViaPiped(videoID string) (title string, duration string, thumbnail string, options []MediaOption) {
	instances := []string{
		"https://pipedapi.kavin.rocks",
		"https://pipedapi.adminforge.de",
		"https://pipedapi.smnz.de",
	}
	for _, inst := range instances {
		apiURL := fmt.Sprintf("%s/streams/%s", inst, videoID)
		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		var vid pipedVideo
		if json.Unmarshal(body, &vid) != nil {
			continue
		}
		if vid.Title == "" || vid.VideoID == "" {
			continue
		}
		title = vid.Title
		if vid.Duration > 0 {
			duration = fmt.Sprintf("%d:%02d", vid.Duration/60, vid.Duration%60)
		}
		thumbnail = vid.ThumbnailURL
		if thumbnail == "" {
			thumbnail = fmt.Sprintf("https://img.youtube.com/vi/%s/maxresdefault.jpg", videoID)
		}
		seen := make(map[string]bool)
		for _, s := range vid.VideoStreams {
			if s.URL == "" || seen[s.URL] {
				continue
			}
			seen[s.URL] = true
			label := fmt.Sprintf("Video %s", s.Quality)
			if s.Quality == "" {
				label = "Video"
			}
			ext := "mp4"
			if s.MimeType != "" {
				ext = detectExtFromURL(s.URL, "mp4")
			}
			options = append(options, MediaOption{
				Quality: label,
				Format:  ext,
				Size:    "Dinamis",
				URL:     s.URL,
			})
		}
		for _, s := range vid.AudioStreams {
			if s.URL == "" || seen[s.URL] {
				continue
			}
			seen[s.URL] = true
			options = append(options, MediaOption{
				Quality: "Audio Only",
				Format:  "m4a",
				Size:    "Dinamis",
				URL:     s.URL,
			})
		}
		if len(options) > 0 {
			log.Printf("[YouTube] Piped instance %s succeeded: %d options\n", inst, len(options))
			return title, duration, thumbnail, options
		}
	}
	return "", "", "", nil
}

// decodeDownloadgramToken decodes the JWT payload from a cdn.downloadgram.org token
// and returns the embedded fields (filename and url). Returns empty strings on failure.
func decodeDownloadgramToken(rawURL string) (filename, cdnURL string) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", ""
	}
	token := parsed.Query().Get("token")
	if token == "" {
		return "", ""
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return "", ""
	}
	payload := parts[1]
	for len(payload)%4 != 0 {
		payload += "="
	}
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		decoded, _ = base64.StdEncoding.DecodeString(payload)
	}
	if len(decoded) == 0 {
		return "", ""
	}
	var jwtPayload struct {
		Filename string `json:"filename"`
		URL      string `json:"url"`
	}
	if json.Unmarshal(decoded, &jwtPayload) != nil {
		return "", ""
	}
	return jwtPayload.Filename, jwtPayload.URL
}

// isDownloadgramVideoToken decodes the JWT payload from a cdn.downloadgram.org token
// and returns true only if the embedded filename ends with a video extension (.mp4).
func isDownloadgramVideoToken(rawURL string) bool {
	filename, _ := decodeDownloadgramToken(rawURL)
	return strings.HasSuffix(strings.ToLower(filename), ".mp4")
}

func fetchInstagramVideos(pageURL string) (title string, duration string, thumbnail string, options []MediaOption) {
	shortcode := extractIGShortcode(pageURL)
	if shortcode == "" {
		log.Printf("[IG Video] Tidak bisa ekstrak shortcode dari: %s\n", pageURL)
		return "", "", "", nil
	}

	// Method 1: Downloadgram
	if t, th, opts := fetchIGVideoViaDownloadgram(pageURL); len(opts) > 0 {
		opts = filterVideoOptions(opts)
		if len(opts) > 0 {
			if d := probeVideoDuration(opts[0].URL); d != "" {
				duration = d
			}
			return t, duration, th, opts
		}
	}

	// Method 2: Snapinsta
	if t, th, opts := fetchIGVideoViaSnapinsta(pageURL); len(opts) > 0 {
		opts = filterVideoOptions(opts)
		if len(opts) > 0 {
			if d := probeVideoDuration(opts[0].URL); d != "" {
				duration = d
			}
			return t, duration, th, opts
		}
	}

	// Method 3: Embed page
	if t, th, opts := fetchIGVideoViaEmbed(shortcode); len(opts) > 0 {
		opts = filterVideoOptions(opts)
		if len(opts) > 0 {
			if d := probeVideoDuration(opts[0].URL); d != "" {
				duration = d
			}
			return t, duration, th, opts
		}
	}

	return "", "", "", nil
}

func fetchIGVideoViaDownloadgram(pageURL string) (title string, thumbnail string, options []MediaOption) {
	form := url.Values{"url": {cleanPageURL(pageURL)}}
	req, err := http.NewRequest("POST", "https://api.downloadgram.org/media", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", nil
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
		log.Printf("[Downloadgram Video] request error: %v\n", err)
		return "", "", nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[Downloadgram Video] HTTP %d, %d bytes (will retry after 2s)\n", resp.StatusCode, len(body))
		time.Sleep(2 * time.Second)
		resp, body, err = doRequest()
		if err != nil {
			log.Printf("[Downloadgram Video] retry error: %v\n", err)
			return "", "", nil
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Printf("[Downloadgram Video] retry HTTP %d, %d bytes\n", resp.StatusCode, len(body))
			return "", "", nil
		}
	}
	html := string(body)
	// downloadgram HTML is embedded in a JS string — convert escape sequences before parsing
	html = strings.ReplaceAll(html, `\x20`, " ")
	html = strings.ReplaceAll(html, `\x22`, `"`)
	html = strings.ReplaceAll(html, `\x27`, "'")

	if t := extractFirstMeta(html, "og:title"); t != "" {
		title = t
	}

	// Extract thumbnail from <img alt="Thumb"> tag (downloadgram uses this, not og:image)
	thumbRe := regexp.MustCompile(`<img[^>]+src="([^"]+)"[^>]*alt="Thumb"`)
	if m := thumbRe.FindStringSubmatch(html); len(m) > 1 {
		thumbnail = decodeHTMLEntities(m[1])
	}
	if thumbnail == "" {
		thumbnail = extractFirstMeta(html, "og:image")
	}

	seen := make(map[string]bool)
	addVideo := func(raw string) {
		if raw == "" || seen[raw] {
			return
		}
		raw = decodeHTMLEntities(strings.TrimSuffix(raw, "\\"))
		if !strings.Contains(raw, "http") {
			return
		}
		seen[raw] = true
		options = append(options, MediaOption{
			Quality: fmt.Sprintf("Video %d - Resolusi Penuh", len(options)+1),
			Format:  "mp4",
			Size:    "Dinamis",
			URL:     raw,
		})
	}

	// cdn.downloadgram.org token URLs — filter via JWT filename to get only video tokens
	reToken := regexp.MustCompile(`https://cdn\.downloadgram\.org/\?token=[A-Za-z0-9._\-]+`)
	for _, u := range reToken.FindAllString(html, -1) {
		u = decodeHTMLEntities(strings.TrimSuffix(u, "\\"))
		if !seen[u] && isDownloadgramVideoToken(u) {
			addVideo(u)
		}
	}
	reVideo := regexp.MustCompile(`https://[^\s"'\\]+(?:cdninstagram|scontent|fbcdn)[^\s"'\\]+`)
	for _, u := range reVideo.FindAllString(html, -1) {
		u = decodeHTMLEntities(strings.TrimSuffix(u, "\\"))
		if seen[u] {
			continue
		}
		if p, e := url.Parse(u); e == nil {
			ext := strings.ToLower(strings.TrimPrefix(path.Ext(p.Path), "."))
			if ext == "mp4" || strings.Contains(p.RawQuery, "mime=video") {
				addVideo(u)
			}
		}
	}

	if len(options) == 0 {
		return "", "", nil
	}
	if title == "" {
		title = "Instagram Video"
	}
	return title, thumbnail, options
}

func fetchIGVideoViaSnapinsta(pageURL string) (title string, thumbnail string, options []MediaOption) {
	client := &http.Client{Timeout: 20 * time.Second}
	homeReq, _ := http.NewRequest("GET", "https://snapinsta.app/", nil)
	homeReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	homeResp, err := client.Do(homeReq)
	if err != nil {
		return "", "", nil
	}
	defer homeResp.Body.Close()
	homeBody, _ := io.ReadAll(homeResp.Body)
	tokenRe := regexp.MustCompile(`name="token"\s+value="([^"]+)"`)
	tokenMatch := tokenRe.FindStringSubmatch(string(homeBody))
	if len(tokenMatch) < 2 {
		return "", "", nil
	}
	token := tokenMatch[1]

	form := url.Values{
		"url":   {cleanPageURL(pageURL)},
		"token": {token},
		"lang":  {"en"},
	}
	postReq, _ := http.NewRequest("POST", "https://snapinsta.app/action.php", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	postReq.Header.Set("Referer", "https://snapinsta.app/")
	postReq.Header.Set("Origin", "https://snapinsta.app")
	postReq.Header.Set("X-Requested-With", "XMLHttpRequest")

	postResp, err := client.Do(postReq)
	if err != nil {
		return "", "", nil
	}
	defer postResp.Body.Close()
	postBody, _ := io.ReadAll(postResp.Body)
	html := string(postBody)

	seen := make(map[string]bool)
	reMP4 := regexp.MustCompile(`href="(https://[^"]+\.mp4[^"]*)"`)
	for _, m := range reMP4.FindAllStringSubmatch(html, -1) {
		if len(m) > 1 {
			u := decodeHTMLEntities(m[1])
			if seen[u] {
				continue
			}
			// Verify path actually ends in .mp4 (not just query params)
			if p, e := url.Parse(u); e == nil {
				ext := strings.ToLower(strings.TrimPrefix(path.Ext(p.Path), "."))
				if ext != "mp4" {
					continue
				}
			}
			seen[u] = true
			options = append(options, MediaOption{
				Quality: fmt.Sprintf("Video %d - HD", len(options)+1),
				Format:  "mp4",
				Size:    "Dinamis",
				URL:     u,
			})
		}
	}
	reCDN := regexp.MustCompile(`data-url="(https://[^"]+(?:cdninstagram|scontent)[^"]+)"`)
	for _, m := range reCDN.FindAllStringSubmatch(html, -1) {
		if len(m) > 1 {
			u := decodeHTMLEntities(m[1])
			if seen[u] {
				continue
			}
			// Only accept if path ends in .mp4 or URL has video indicators
			isVideo := false
			if p, e := url.Parse(u); e == nil {
				ext := strings.ToLower(strings.TrimPrefix(path.Ext(p.Path), "."))
				isVideo = ext == "mp4" || strings.Contains(p.RawQuery, "mime=video") ||
					strings.Contains(u, "/video/") || strings.Contains(u, "video_dashinit")
			}
			if isVideo {
				seen[u] = true
				options = append(options, MediaOption{
					Quality: fmt.Sprintf("Video %d - HD", len(options)+1),
					Format:  "mp4",
					Size:    "Dinamis",
					URL:     u,
				})
			}
		}
	}

	thumbRe := regexp.MustCompile(`<img[^>]+class="[^"]*thumb[^"]*"[^>]+src="([^"]+)"`)
	if m := thumbRe.FindStringSubmatch(html); len(m) > 1 {
		thumbnail = decodeHTMLEntities(m[1])
	}
	if len(options) == 0 {
		return "", "", nil
	}
	return "Instagram Video", thumbnail, options
}

func fetchIGVideoViaEmbed(shortcode string) (title string, thumbnail string, options []MediaOption) {
	embedURL := fmt.Sprintf("https://www.instagram.com/reel/%s/embed/", shortcode)
	html, err := fetchPageHTML(embedURL, "https://www.instagram.com/")
	if err != nil {
		embedURL = fmt.Sprintf("https://www.instagram.com/p/%s/embed/", shortcode)
		html, err = fetchPageHTML(embedURL, "https://www.instagram.com/")
		if err != nil {
			return "", "", nil
		}
	}

	videoURL := extractFirstMeta(html, "og:video:secure_url", "og:video")
	if videoURL == "" {
		reVideo := regexp.MustCompile(`"video_url"\s*:\s*"([^"]+)"`)
		if m := reVideo.FindStringSubmatch(html); len(m) > 1 {
			videoURL = strings.ReplaceAll(m[1], `\u0026`, "&")
		}
	}
	if videoURL == "" {
		return "", "", nil
	}

	title = extractFirstMeta(html, "og:title")
	if title == "" {
		title = "Instagram Video"
	}
	thumbnail = extractFirstMeta(html, "og:image")

	options = append(options, MediaOption{
		Quality: "Video - Resolusi Penuh",
		Format:  "mp4",
		Size:    "Dinamis",
		URL:     videoURL,
	})
	return title, thumbnail, options
}

func fetchTikTokVideos(pageURL string) (title string, duration string, thumbnail string, options []MediaOption) {
	resolvedURL := resolveTikTokURL(pageURL)
	log.Printf("[TikTok Video] Resolved URL: %s\n", resolvedURL)

	// Method 1: TikWM API
	if t, d, th, opts := fetchTikTokVideoViaTikWM(resolvedURL); len(opts) > 0 {
		return t, d, th, opts
	}

	// Method 2: Page scraping with SIGI_STATE / og:video
	if t, th, opts := fetchTikTokVideoViaPage(resolvedURL); len(opts) > 0 {
		d := ""
		if len(opts) > 0 {
			if dd := probeVideoDuration(opts[0].URL); dd != "" {
				d = dd
			}
		}
		return t, d, th, opts
	}

	return "", "", "", nil
}

type TikWMResponse struct {
	Code int       `json:"code"`
	Msg  string    `json:"msg"`
	Data TikWMData `json:"data"`
}

type TikWMData struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Cover     string      `json:"cover"`
	Duration  int         `json:"duration"`
	Play      string      `json:"play"`
	HDPlay    string      `json:"hdplay"`
	WMPlay    string      `json:"wmplay"`
	Music     string      `json:"music"`
	Images    []string    `json:"images"`
	Author    TikWMAuthor `json:"author"`
}

type TikWMAuthor struct {
	ID       string `json:"id"`
	UniqueID string `json:"unique_id"`
	Nickname string `json:"nickname"`
}

func fetchTikTokVideoViaTikWM(pageURL string) (title string, duration string, thumbnail string, options []MediaOption) {
	form := url.Values{
		"url": {pageURL},
		"hd":  {"1"},
	}
	req, err := http.NewRequest("POST", "https://www.tikwm.com/api/", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", "", nil
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.tikwm.com/")
	req.Header.Set("Origin", "https://www.tikwm.com")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", nil
	}

	var tikwm TikWMResponse
	if err := json.Unmarshal(body, &tikwm); err != nil {
		return "", "", "", nil
	}
	if tikwm.Code != 0 {
		return "", "", "", nil
	}

	data := tikwm.Data
	title = data.Title
	if title == "" {
		title = fmt.Sprintf("TikTok @%s", data.Author.UniqueID)
	}
	if len(title) > 100 {
		title = title[:100] + "..."
	}

	normalizeURL := func(u string) string {
		if u == "" {
			return ""
		}
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			return u
		}
		return "https://www.tikwm.com" + u
	}

	thumbnail = normalizeURL(data.Cover)

	if data.HDPlay != "" {
		options = append(options, MediaOption{
			Quality: "Video No Watermark (HD)",
			Format:  "mp4",
			Size:    "Dinamis",
			URL:     normalizeURL(data.HDPlay),
		})
	}
	if data.Play != "" {
		options = append(options, MediaOption{
			Quality: "Video No Watermark (SD)",
			Format:  "mp4",
			Size:    "Dinamis",
			URL:     normalizeURL(data.Play),
		})
	}
	if data.WMPlay != "" {
		options = append(options, MediaOption{
			Quality: "Video Watermarked",
			Format:  "mp4",
			Size:    "Dinamis",
			URL:     normalizeURL(data.WMPlay),
		})
	}
	if data.Music != "" {
		options = append(options, MediaOption{
			Quality: "Audio Only (MP3)",
			Format:  "mp3",
			Size:    "Dinamis",
			URL:     normalizeURL(data.Music),
		})
	}

	if len(options) == 0 {
		return "", "", "", nil
	}
	if data.Duration > 0 {
		duration = fmt.Sprintf("%d:%02d", data.Duration/60, data.Duration%60)
	}
	return title, duration, thumbnail, options
}

func fetchTikTokVideoViaPage(pageURL string) (title string, thumbnail string, options []MediaOption) {
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return "", "", nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	title = extractFirstMeta(html, "og:title", "twitter:title")
	if title == "" {
		title = "TikTok Video"
	}
	thumbnail = extractFirstMeta(html, "og:image", "twitter:image")

	videoURL := extractFirstMeta(html, "og:video:secure_url", "og:video", "twitter:player")
	if videoURL == "" {
		return "", "", nil
	}

	options = append(options, MediaOption{
		Quality: "Video - Resolusi Penuh",
		Format:  "mp4",
		Size:    "Dinamis",
		URL:     videoURL,
	})
	return title, thumbnail, options
}

func fetchFacebookVideos(pageURL string) (title string, duration string, thumbnail string, options []MediaOption) {
	html, err := fetchPageHTML(pageURL, "https://www.facebook.com/")
	if err != nil {
		return "", "", "", nil
	}

	title = extractFirstMeta(html, "og:title", "twitter:title")
	if title == "" {
		title = "Facebook Video"
	}
	thumbnail = extractFirstMeta(html, "og:image:secure_url", "og:image", "twitter:image")
	videoURL := extractFirstMeta(html, "og:video:secure_url", "og:video", "og:video:url")
	if videoURL == "" {
		return "", "", "", nil
	}

	options = append(options, MediaOption{
		Quality: "Video Resolusi Penuh",
		Format:  "mp4",
		Size:    "Dinamis",
		URL:     videoURL,
	})
	if d := probeVideoDuration(videoURL); d != "" {
		duration = d
	}
	return title, duration, thumbnail, options
}

func findFFmpeg() string {
	if p, err := exec.LookPath("ffmpeg"); err == nil && p != "" {
		return p
	}
	base := os.Getenv("LOCALAPPDATA")
	candidates := []string{
		filepath.Join(base, "Microsoft", "WinGet", "Packages", "Gyan.FFmpeg_Microsoft.Winget.Source_8wekyb3d8bbwe", "ffmpeg-8.1.1-full_build", "bin", "ffmpeg.exe"),
		filepath.Join(base, "Microsoft", "WinGet", "Packages", "Gyan.FFmpeg.Essentials_Microsoft.Winget.Source_8wekyb3d8bbwe", "ffmpeg-8.1.1-essentials_build", "bin", "ffmpeg.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "ffmpeg"
}

var (
	loadedCookies string
)

func findCookiesFile() string {
	if loadedCookies != "" {
		return loadedCookies
	}
	for _, name := range []string{"cookies.txt", "yt-cookies.txt", ".cookies.txt"} {
		if _, err := os.Stat(name); err == nil {
			loadedCookies = name
			log.Printf("[yt-dlp] found cookies file: %s\n", name)
			return name
		}
	}
	return ""
}

func cookieArgs() []string {
	if p := findCookiesFile(); p != "" {
		return []string{"--cookies", p}
	}
	return nil
}

func proxyArgs() []string {
	if p := os.Getenv("HTTP_PROXY"); p != "" {
		return []string{"--proxy", p}
	}
	if p := os.Getenv("HTTPS_PROXY"); p != "" {
		return []string{"--proxy", p}
	}
	return nil
}

func findFFprobe() string {
	if p, err := exec.LookPath("ffprobe"); err == nil && p != "" {
		return p
	}
	base := os.Getenv("LOCALAPPDATA")
	candidates := []string{
		filepath.Join(base, "Microsoft", "WinGet", "Packages", "Gyan.FFmpeg_Microsoft.Winget.Source_8wekyb3d8bbwe", "ffmpeg-8.1.1-full_build", "bin", "ffprobe.exe"),
		filepath.Join(base, "Microsoft", "WinGet", "Packages", "Gyan.FFmpeg.Essentials_Microsoft.Winget.Source_8wekyb3d8bbwe", "ffmpeg-8.1.1-essentials_build", "bin", "ffprobe.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "ffprobe"
}

func probeVideoDuration(videoURL string) string {
	ffprobePath := findFFprobe()
	if _, err := os.Stat(ffprobePath); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		"-user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
		"-headers", "Referer: https://www.instagram.com/\r\n",
		videoURL,
	)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	s := strings.TrimSpace(string(out))
	seconds, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return ""
	}
	mins := int(seconds) / 60
	secs := int(seconds) % 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}
