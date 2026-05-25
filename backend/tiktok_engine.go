package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ─── TikTok API Structs ───────────────────────────────────────────────────────

type TikTokAPIResponse struct {
	StatusCode int            `json:"statusCode"`
	ItemInfo   TikTokItemInfo `json:"itemInfo"`
}

type TikTokItemInfo struct {
	ItemStruct TikTokItem `json:"itemStruct"`
}

type TikTokItem struct {
	ID        string           `json:"id"`
	Desc      string           `json:"desc"`
	ImagePost *TikTokImagePost `json:"imagePost"`
	Video     TikTokVideo      `json:"video"`
	Author    TikTokAuthor     `json:"author"`
}

type TikTokImagePost struct {
	Images []TikTokImage `json:"images"`
}

type TikTokImage struct {
	ImageURL TikTokImageURL `json:"imageURL"`
}

type TikTokImageURL struct {
	URLList []string `json:"urlList"`
}

type TikTokVideo struct {
	PlayAddr     TikTokAddr `json:"playAddr"`
	DownloadAddr TikTokAddr `json:"downloadAddr"`
	Cover        string     `json:"cover"`
	DynamicCover string     `json:"dynamicCover"`
}

type TikTokAddr struct {
	URLList []string `json:"urlList"`
}

type TikTokAuthor struct {
	UniqueID string `json:"uniqueId"`
	Nickname string `json:"nickname"`
}

// ─── TikTok oEmbed response ───────────────────────────────────────────────────

type TikTokOEmbedResponse struct {
	Title        string `json:"title"`
	AuthorName   string `json:"author_name"`
	ThumbnailURL string `json:"thumbnail_url"`
}

// ─── Resolve TikTok short URL ─────────────────────────────────────────────────

func resolveTikTokURL(rawURL string) string {
	if !strings.Contains(rawURL, "vm.tiktok.com") && !strings.Contains(rawURL, "vt.tiktok.com") {
		return rawURL
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // stop at first redirect
		},
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		return rawURL
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc != "" {
		return loc
	}
	return rawURL
}

// ─── Extract TikTok video ID from URL ────────────────────────────────────────

func extractTikTokVideoID(pageURL string) string {
	re := regexp.MustCompile(`/video/(\d+)`)
	m := re.FindStringSubmatch(pageURL)
	if len(m) > 1 {
		return m[1]
	}
	// Try photo post
	re2 := regexp.MustCompile(`/photo/(\d+)`)
	m2 := re2.FindStringSubmatch(pageURL)
	if len(m2) > 1 {
		return m2[1]
	}
	return ""
}

// ─── Method 1: TikTok internal API ───────────────────────────────────────────

func fetchTikTokViaAPI(videoID string) (title string, thumbnail string, options []MediaOption, err error) {
	apiURL := fmt.Sprintf("https://www.tiktok.com/api/item/detail/?itemId=%s&aid=1988", videoID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", "", nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://www.tiktok.com/")
	req.Header.Set("Cookie", "tt_webid_v2=1; ttwid=1")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", nil, fmt.Errorf("TikTok API status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, err
	}

	var apiResp TikTokAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", "", nil, err
	}

	item := apiResp.ItemInfo.ItemStruct
	if item.ID == "" {
		return "", "", nil, fmt.Errorf("empty item from TikTok API")
	}

	title = item.Desc
	if title == "" {
		title = fmt.Sprintf("TikTok @%s", item.Author.UniqueID)
	}
	if len(title) > 80 {
		title = title[:80] + "..."
	}

	seen := make(map[string]bool)
	addPhoto := func(imgURL, label string) {
		if imgURL == "" {
			return
		}
		key := normalizeURLForDedup(imgURL)
		if seen[key] {
			return
		}
		seen[key] = true
		if thumbnail == "" {
			thumbnail = imgURL
		}
		options = append(options, MediaOption{
			Quality: label,
			Format:  "jpg",
			Size:    "Dinamis",
			URL:     imgURL,
		})
	}

	// Photo slideshow
	if item.ImagePost != nil && len(item.ImagePost.Images) > 0 {
		for idx, img := range item.ImagePost.Images {
			label := fmt.Sprintf("Foto %d - Resolusi Penuh", idx+1)
			if len(img.ImageURL.URLList) > 0 {
				addPhoto(img.ImageURL.URLList[0], label)
			}
		}
	} else if item.Video.Cover != "" {
		// Video thumbnail
		thumbnail = item.Video.Cover
		addPhoto(item.Video.Cover, "Cover / Thumbnail")
	}

	if len(options) == 0 {
		return "", "", nil, fmt.Errorf("no images found in TikTok API response")
	}

	return title, thumbnail, options, nil
}

// ─── Method 2: TikTok embed page scraping ────────────────────────────────────

func fetchTikTokViaEmbed(pageURL string) (title string, thumbnail string, options []MediaOption, err error) {
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return "", "", nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, err
	}
	html := string(body)

	// Extract SIGI_STATE JSON dari halaman TikTok
	sigiRe := regexp.MustCompile(`<script id="SIGI_STATE"[^>]*>(.*?)</script>`)
	sigiMatch := sigiRe.FindStringSubmatch(html)
	if len(sigiMatch) > 1 {
		sigiJSON := sigiMatch[1]

		// Extract image URLs dari SIGI_STATE
		imgRe := regexp.MustCompile(`"urlList"\s*:\s*\[([^\]]+)\]`)
		imgMatches := imgRe.FindAllStringSubmatch(sigiJSON, -1)

		seen := make(map[string]bool)
		photoIdx := 0
		for _, m := range imgMatches {
			if len(m) < 2 {
				continue
			}
			// Parse URL list
			urlListStr := m[1]
			urlRe := regexp.MustCompile(`"(https://[^"]+)"`)
			urlMatches := urlRe.FindAllStringSubmatch(urlListStr, -1)
			for _, um := range urlMatches {
				if len(um) < 2 {
					continue
				}
				u := strings.ReplaceAll(um[1], `\u0026`, "&")
				// Filter hanya URL gambar (bukan video)
				if strings.Contains(u, "tiktokcdn") || strings.Contains(u, "tiktok-obj") {
					if strings.Contains(u, ".mp4") || strings.Contains(u, "video") {
						continue
					}
					key := normalizeURLForDedup(u)
					if seen[key] {
						continue
					}
					seen[key] = true
					photoIdx++
					label := fmt.Sprintf("Foto %d - Resolusi Penuh", photoIdx)
					if thumbnail == "" {
						thumbnail = u
					}
					options = append(options, MediaOption{
						Quality: label,
						Format:  "jpg",
						Size:    "Dinamis",
						URL:     u,
					})
				}
			}
		}
	}

	// Fallback: og:image
	if len(options) == 0 {
		ogImage := extractFirstMeta(html, "og:image")
		if ogImage != "" {
			thumbnail = ogImage
			options = append(options, MediaOption{
				Quality: "Foto Cover",
				Format:  "jpg",
				Size:    "Dinamis",
				URL:     ogImage,
			})
		}
	}

	// Title dari og:title
	title = extractFirstMeta(html, "og:title", "twitter:title")
	if title == "" {
		title = "TikTok Photo"
	}

	if len(options) == 0 {
		return "", "", nil, fmt.Errorf("no images found in TikTok page")
	}

	return title, thumbnail, options, nil
}

// ─── Method 3: TikTok oEmbed ─────────────────────────────────────────────────

func fetchTikTokViaOEmbed(pageURL string) (title string, thumbnail string, options []MediaOption, err error) {
	apiURL := fmt.Sprintf("https://www.tiktok.com/oembed?url=%s", url.QueryEscape(pageURL))

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", "", nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, err
	}

	var oembed TikTokOEmbedResponse
	if err := json.Unmarshal(body, &oembed); err != nil {
		return "", "", nil, err
	}

	if oembed.ThumbnailURL == "" {
		return "", "", nil, fmt.Errorf("no thumbnail from TikTok oEmbed")
	}

	title = oembed.Title
	if title == "" {
		title = fmt.Sprintf("TikTok @%s", oembed.AuthorName)
	}
	thumbnail = oembed.ThumbnailURL
	options = append(options, MediaOption{
		Quality: "Foto Cover / Thumbnail",
		Format:  "jpg",
		Size:    "Dinamis",
		URL:     oembed.ThumbnailURL,
	})

	return title, thumbnail, options, nil
}

// ─── Main TikTok image engine ─────────────────────────────────────────────────

func fetchTikTokImages(pageURL string) (title string, thumbnail string, options []MediaOption) {
	resolvedURL := resolveTikTokURL(pageURL)
	log.Printf("[TikTok] Resolved URL: %s\n", resolvedURL)

	// Method 1: TikWM API — handles both photo slideshows and video covers
	log.Printf("[TikTok] Mencoba TikWM API...\n")
	if t, th, opts := fetchTikTokImagesViaTikWM(resolvedURL); len(opts) > 0 {
		log.Printf("[TikTok] TikWM berhasil: %d foto\n", len(opts))
		return t, th, opts
	}
	log.Printf("[TikTok] TikWM gagal\n")

	// Method 2: Internal API (photo slideshow via imagePost field)
	videoID := extractTikTokVideoID(resolvedURL)
	if videoID != "" {
		log.Printf("[TikTok] Mencoba internal API...\n")
		t, th, opts, err := fetchTikTokViaAPI(videoID)
		if err == nil && len(opts) > 0 {
			log.Printf("[TikTok] Internal API berhasil: %d foto\n", len(opts))
			return t, th, opts
		}
		log.Printf("[TikTok] Internal API gagal: %v\n", err)
	}

	// Method 3: Page scraping
	log.Printf("[TikTok] Mencoba page scraping...\n")
	t, th, opts, err := fetchTikTokViaEmbed(resolvedURL)
	if err == nil && len(opts) > 0 {
		log.Printf("[TikTok] Page scraping berhasil: %d foto\n", len(opts))
		return t, th, opts
	}
	log.Printf("[TikTok] Page scraping gagal: %v\n", err)

	// Method 4: oEmbed (thumbnail only fallback)
	log.Printf("[TikTok] Mencoba oEmbed...\n")
	t, th, opts, err = fetchTikTokViaOEmbed(resolvedURL)
	if err == nil && len(opts) > 0 {
		log.Printf("[TikTok] oEmbed berhasil: %d foto\n", len(opts))
		return t, th, opts
	}
	log.Printf("[TikTok] oEmbed gagal: %v\n", err)

	return "", "", nil
}

// fetchTikTokImagesViaTikWM uses the TikWM API to get photo slideshow images.
// For photo posts: returns all images in the slideshow.
// For video posts: returns the cover thumbnail.
func fetchTikTokImagesViaTikWM(pageURL string) (title string, thumbnail string, options []MediaOption) {
	form := url.Values{
		"url": {pageURL},
		"hd":  {"1"},
	}
	req, err := http.NewRequest("POST", "https://www.tikwm.com/api/", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", nil
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.tikwm.com/")
	req.Header.Set("Origin", "https://www.tikwm.com")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[TikWM Image] request error: %v\n", err)
		return "", "", nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil
	}

	// TikWM response — reuse the struct from video_engine.go
	var tikwm TikWMResponse
	if err := json.Unmarshal(body, &tikwm); err != nil {
		log.Printf("[TikWM Image] JSON parse error: %v\n", err)
		return "", "", nil
	}
	if tikwm.Code != 0 {
		log.Printf("[TikWM Image] API error code %d: %s\n", tikwm.Code, tikwm.Msg)
		return "", "", nil
	}

	data := tikwm.Data
	title = data.Title
	if title == "" {
		title = fmt.Sprintf("TikTok @%s", data.Author.UniqueID)
	}
	if len(title) > 100 {
		title = title[:100] + "..."
	}

	// Convert relative URLs to absolute TikWM URLs
	tikwmBase := "https://www.tikwm.com"
	normalizeURL := func(u string) string {
		if u == "" {
			return ""
		}
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			return u
		}
		return tikwmBase + u
	}

	thumbnail = normalizeURL(data.Cover)

	// Photo slideshow — images array contains the actual photos
	if len(data.Images) > 0 {
		for i, imgURL := range data.Images {
			if imgURL == "" {
				continue
			}
			options = append(options, MediaOption{
				Quality: fmt.Sprintf("Foto %d - Resolusi Penuh", i+1),
				Format:  "jpg",
				Size:    "Dinamis",
				URL:     normalizeURL(imgURL),
			})
		}
		return title, thumbnail, options
	}

	// Video post — return cover as thumbnail image
	if data.Cover != "" {
		options = append(options, MediaOption{
			Quality: "Cover / Thumbnail",
			Format:  "jpg",
			Size:    "Dinamis",
			URL:     normalizeURL(data.Cover),
		})
		return title, thumbnail, options
	}

	return "", "", nil
}
