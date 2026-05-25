package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ─── Instagram API Structs ────────────────────────────────────────────────────

type IGMediaResponse struct {
	Items []IGItem `json:"items"`
}

type IGItem struct {
	MediaType     int           `json:"media_type"` // 1=photo, 2=video, 8=carousel
	ImageVersions IGImageVers   `json:"image_versions2"`
	VideoVersions []IGVideo     `json:"video_versions"`
	CarouselMedia []IGItem      `json:"carousel_media"`
	User          IGUser        `json:"user"`
	Caption       IGCaption     `json:"caption"`
	ID            string        `json:"id"`
}

type IGImageVers struct {
	Candidates []IGImageCandidate `json:"candidates"`
}

type IGImageCandidate struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type IGVideo struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Type   int    `json:"type"`
}

type IGUser struct {
	Username string `json:"username"`
}

type IGCaption struct {
	Text string `json:"text"`
}

type IGGraphQLResponse struct {
	Data IGGraphQLData `json:"data"`
}

type IGGraphQLData struct {
	XDTShortcodeMedia *IGShortcodeMedia `json:"xdt_shortcode_media"`
}

type IGShortcodeMedia struct {
	Typename              string              `json:"__typename"`
	DisplayURL            string              `json:"display_url"`
	DisplayResources      []IGDisplayResource `json:"display_resources"`
	EdgeSidecarToChildren *IGEdgeSidecar      `json:"edge_sidecar_to_children"`
	IsVideo               bool                `json:"is_video"`
	VideoURL              string              `json:"video_url"`
	Owner                 IGOwner             `json:"owner"`
	EdgeMediaToCaption    IGEdgeCaption       `json:"edge_media_to_caption"`
}

type IGDisplayResource struct {
	Src          string `json:"src"`
	ConfigWidth  int    `json:"config_width"`
	ConfigHeight int    `json:"config_height"`
}

type IGEdgeSidecar struct {
	Edges []IGEdge `json:"edges"`
}

type IGEdge struct {
	Node IGEdgeNode `json:"node"`
}

type IGEdgeNode struct {
	DisplayURL       string              `json:"display_url"`
	DisplayResources []IGDisplayResource `json:"display_resources"`
	IsVideo          bool                `json:"is_video"`
	VideoURL         string              `json:"video_url"`
}

type IGOwner struct {
	Username string `json:"username"`
}

type IGEdgeCaption struct {
	Edges []struct {
		Node struct {
			Text string `json:"text"`
		} `json:"node"`
	} `json:"edges"`
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func igHTTPClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second}
}

func igHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("X-IG-App-ID", "936619743392459")
	req.Header.Set("X-ASBD-ID", "129477")
	req.Header.Set("X-IG-WWW-Claim", "0")
	req.Header.Set("Origin", "https://www.instagram.com")
	req.Header.Set("Referer", "https://www.instagram.com/")
}

func extractIGShortcode(pageURL string) string {
	patterns := []string{
		`/p/([A-Za-z0-9_\-]+)`,
		`/reel/([A-Za-z0-9_\-]+)`,
		`/reels/([A-Za-z0-9_\-]+)`,
		`/tv/([A-Za-z0-9_\-]+)`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		m := re.FindStringSubmatch(pageURL)
		if len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func igShortcodeToID(shortcode string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var id int64
	for _, c := range shortcode {
		idx := strings.IndexRune(alphabet, c)
		if idx < 0 {
			return ""
		}
		id = id*64 + int64(idx)
	}
	return fmt.Sprintf("%d", id)
}

// ─── Method 1: GraphQL API ────────────────────────────────────────────────────

func fetchIGViaGraphQL(shortcode string) (title string, thumbnail string, options []MediaOption, err error) {
	queryHash := "9f8827793ef34641b2fb195d4d41151c"
	apiURL := fmt.Sprintf(
		"https://www.instagram.com/graphql/query/?query_hash=%s&variables={\"shortcode\":\"%s\"}",
		queryHash, shortcode,
	)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", "", nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("X-IG-App-ID", "936619743392459")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", fmt.Sprintf("https://www.instagram.com/p/%s/", shortcode))

	resp, err := igHTTPClient().Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", nil, fmt.Errorf("GraphQL status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, err
	}
	var gqlResp IGGraphQLResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return "", "", nil, err
	}
	media := gqlResp.Data.XDTShortcodeMedia
	if media == nil {
		return "", "", nil, fmt.Errorf("no media in GraphQL response")
	}
	if len(media.EdgeMediaToCaption.Edges) > 0 {
		text := media.EdgeMediaToCaption.Edges[0].Node.Text
		if len(text) > 80 {
			text = text[:80] + "..."
		}
		title = text
	}
	if title == "" {
		title = fmt.Sprintf("Instagram Post @%s", media.Owner.Username)
	}
	seen := make(map[string]bool)
	addPhoto := func(imgURL, label string) {
		if imgURL == "" {
			return
		}
		key := normalizeURLForDedup(imgURL)
		if seen[key] {
			log.Printf("[IG] Skipping duplicate URL: %s\n", key)
			return
		}
		seen[key] = true
		if thumbnail == "" {
			thumbnail = imgURL
		}
		options = append(options, MediaOption{Quality: label, Format: "jpg", Size: "Dinamis", URL: imgURL})
	}
	if media.EdgeSidecarToChildren != nil && len(media.EdgeSidecarToChildren.Edges) > 0 {
		photoCount := 0
		for _, edge := range media.EdgeSidecarToChildren.Edges {
			bestURL := edge.Node.DisplayURL
			bestWidth := 0
			for _, res := range edge.Node.DisplayResources {
				if res.ConfigWidth > bestWidth {
					bestWidth = res.ConfigWidth
					bestURL = res.Src
				}
			}
			if bestURL != "" {
				photoCount++
				label := fmt.Sprintf("Foto %d - Resolusi Penuh", photoCount)
				addPhoto(bestURL, label)
			}
		}
	} else {
		bestURL := media.DisplayURL
		bestWidth := 0
		for _, res := range media.DisplayResources {
			if res.ConfigWidth > bestWidth {
				bestWidth = res.ConfigWidth
				bestURL = res.Src
			}
		}
		addPhoto(bestURL, "Foto Resolusi Penuh")
	}
	return title, thumbnail, options, nil
}

// ─── Method 2: Mobile API ─────────────────────────────────────────────────────

func fetchIGViaMobileAPI(shortcode string) (title string, thumbnail string, options []MediaOption, err error) {
	mediaID := igShortcodeToID(shortcode)
	if mediaID == "" {
		return "", "", nil, fmt.Errorf("cannot convert shortcode to ID")
	}
	apiURL := fmt.Sprintf("https://i.instagram.com/api/v1/media/%s/info/", mediaID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", "", nil, err
	}
	igHeaders(req)
	resp, err := igHTTPClient().Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", nil, fmt.Errorf("mobile API status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, err
	}
	var mediaResp IGMediaResponse
	if err := json.Unmarshal(body, &mediaResp); err != nil {
		return "", "", nil, err
	}
	if len(mediaResp.Items) == 0 {
		return "", "", nil, fmt.Errorf("no items in mobile API response")
	}
	item := mediaResp.Items[0]
	if item.Caption.Text != "" {
		text := item.Caption.Text
		if len(text) > 80 {
			text = text[:80] + "..."
		}
		title = text
	}
	if title == "" {
		title = fmt.Sprintf("Instagram Post @%s", item.User.Username)
	}
	seen := make(map[string]bool)
	addBestPhoto := func(imgVers IGImageVers, label string) {
		bestURL := ""
		bestWidth := 0
		for _, c := range imgVers.Candidates {
			if c.Width > bestWidth {
				bestWidth = c.Width
				bestURL = c.URL
			}
		}
		if bestURL == "" {
			return
		}
		key := normalizeURLForDedup(bestURL)
		if seen[key] {
			log.Printf("[IG Mobile] Skipping duplicate URL: %s\n", key)
			return
		}
		seen[key] = true
		if thumbnail == "" {
			thumbnail = bestURL
		}
		options = append(options, MediaOption{Quality: label, Format: "jpg", Size: "Dinamis", URL: bestURL})
	}
	if item.MediaType == 8 && len(item.CarouselMedia) > 0 {
		photoCount := 0
		for _, child := range item.CarouselMedia {
			bestURL := ""
			bestWidth := 0
			for _, c := range child.ImageVersions.Candidates {
				if c.Width > bestWidth {
					bestWidth = c.Width
					bestURL = c.URL
				}
			}
			if bestURL != "" {
				photoCount++
				addBestPhoto(child.ImageVersions, fmt.Sprintf("Foto %d - Resolusi Penuh", photoCount))
			}
		}
	} else {
		addBestPhoto(item.ImageVersions, "Foto Resolusi Penuh")
	}
	return title, thumbnail, options, nil
}

// ─── Method 3: Embed JSON (facebookexternalhit UA) ───────────────────────────

func fetchIGViaEmbedJSON(shortcode string) (title string, thumbnail string, options []MediaOption, err error) {
	embedURL := fmt.Sprintf("https://www.instagram.com/p/%s/embed/", shortcode)
	req, err := http.NewRequest("GET", embedURL, nil)
	if err != nil {
		return "", "", nil, err
	}
	req.Header.Set("User-Agent", "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, err
	}
	html := string(body)

	seen := make(map[string]bool)
	photoCount := 0
	addPhoto := func(imgURL string) {
		if imgURL == "" {
			return
		}
		imgURL = strings.ReplaceAll(imgURL, `\u0026`, "&")
		imgURL = decodeHTMLEntities(imgURL)
		if strings.Contains(imgURL, "s150x150") || strings.Contains(imgURL, "s320x320") ||
			strings.Contains(imgURL, "profile_pic") {
			return
		}
		key := normalizeURLForDedup(imgURL)
		if seen[key] {
			log.Printf("[IG Embed JSON] Skipping duplicate URL: %s\n", key)
			return
		}
		seen[key] = true
		photoCount++
		label := fmt.Sprintf("Foto %d - Resolusi Penuh", photoCount)
		if thumbnail == "" {
			thumbnail = imgURL
		}
		options = append(options, MediaOption{Quality: label, Format: "jpg", Size: "Dinamis", URL: imgURL})
	}

	re1 := regexp.MustCompile(`"display_url"\s*:\s*"(https://[^"]+)"`)
	for _, m := range re1.FindAllStringSubmatch(html, -1) {
		if len(m) > 1 {
			addPhoto(m[1])
		}
	}
	if len(options) == 0 {
		re2 := regexp.MustCompile(`<img[^>]+src="(https://[^"]*(?:scontent|cdninstagram)[^"]+)"`)
		for _, m := range re2.FindAllStringSubmatch(html, -1) {
			if len(m) > 1 {
				addPhoto(m[1])
			}
		}
	}
	if len(options) == 0 {
		return "", "", nil, fmt.Errorf("no images in embed JSON")
	}
	if len(options) > 1 {
		for i := range options {
			options[i].Quality = fmt.Sprintf("Foto %d - Resolusi Penuh", i+1)
		}
	}
	title = extractFirstMeta(html, "og:title")
	if title == "" {
		title = "Instagram Post"
	}
	return title, thumbnail, options, nil
}

// ─── Method 4: Embed page scraping (Chrome UA) ───────────────────────────────

func fetchIGViaEmbed(shortcode string) (title string, thumbnail string, options []MediaOption, err error) {
	embedURL := fmt.Sprintf("https://www.instagram.com/p/%s/embed/captioned/", shortcode)
	req, err := http.NewRequest("GET", embedURL, nil)
	if err != nil {
		return "", "", nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://www.instagram.com/")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", nil, fmt.Errorf("embed status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, err
	}
	html := string(body)

	seen := make(map[string]bool)
	photoCount := 0
	addPhoto := func(imgURL, label string) {
		if imgURL == "" {
			return
		}
		imgURL = strings.ReplaceAll(imgURL, `\u0026`, "&")
		imgURL = decodeHTMLEntities(imgURL)
		if strings.Contains(imgURL, "s150x150") || strings.Contains(imgURL, "s320x320") ||
			strings.Contains(imgURL, "profile_pic") || strings.Contains(imgURL, "emoji") {
			return
		}
		key := normalizeURLForDedup(imgURL)
		if seen[key] {
			log.Printf("[IG Embed Scraping] Skipping duplicate URL: %s\n", key)
			return
		}
		seen[key] = true
		photoCount++
		if label == "" {
			label = fmt.Sprintf("Foto %d - Resolusi Penuh", photoCount)
		}
		if thumbnail == "" {
			thumbnail = imgURL
		}
		options = append(options, MediaOption{Quality: label, Format: "jpg", Size: "Dinamis", URL: imgURL})
	}

	patterns := []string{
		`"display_url"\s*:\s*"(https://[^"]+)"`,
		`"src"\s*:\s*"(https://[^"]+(?:scontent|cdninstagram)[^"]+)"`,
		`<img[^>]+src="(https://[^"]+(?:scontent|cdninstagram)[^"]+)"`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		for _, m := range re.FindAllStringSubmatch(html, -1) {
			if len(m) > 1 {
				addPhoto(m[1], fmt.Sprintf("Foto %d - Resolusi Penuh", len(options)+1))
			}
		}
		if len(options) > 0 {
			break
		}
	}
	if len(options) > 1 {
		for i := range options {
			options[i].Quality = fmt.Sprintf("Foto %d - Resolusi Penuh", i+1)
		}
	}
	captionRe := regexp.MustCompile(`"text"\s*:\s*"([^"]{10,})"`)
	if m := captionRe.FindStringSubmatch(html); len(m) > 1 {
		text := m[1]
		if len(text) > 80 {
			text = text[:80] + "..."
		}
		title = text
	}
	if title == "" {
		title = "Instagram Post"
	}
	if len(options) == 0 {
		return "", "", nil, fmt.Errorf("no images found in embed page")
	}
	return title, thumbnail, options, nil
}

// ─── Method 5: Public viewer (picuki/imginn) ─────────────────────────────────

func fetchIGViaPublicViewer(shortcode string) (title string, thumbnail string, options []MediaOption, err error) {
	viewers := []struct {
		name   string
		urlFmt string
		imgPat string
	}{
		{
			name:   "picuki",
			urlFmt: "https://www.picuki.com/media/%s",
			imgPat: `<img[^>]+class="[^"]*post-image[^"]*"[^>]+src="([^"]+)"`,
		},
		{
			name:   "imginn",
			urlFmt: "https://imginn.com/p/%s/",
			imgPat: `<img[^>]+class="[^"]*swiper-img[^"]*"[^>]+src="([^"]+)"`,
		},
	}
	for _, viewer := range viewers {
		viewerURL := fmt.Sprintf(viewer.urlFmt, shortcode)
		req, reqErr := http.NewRequest("GET", viewerURL, nil)
		if reqErr != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")

		client := &http.Client{Timeout: 15 * time.Second}
		resp, respErr := client.Do(req)
		if respErr != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			continue
		}
		html := string(body)

		re := regexp.MustCompile(viewer.imgPat)
		seen := make(map[string]bool)
		photoCount := 0
		var localOpts []MediaOption
		for _, m := range re.FindAllStringSubmatch(html, -1) {
			if len(m) < 2 {
				continue
			}
			imgURL := decodeHTMLEntities(m[1])
			key := normalizeURLForDedup(imgURL)
			if seen[key] {
				log.Printf("[IG Public Viewer] Skipping duplicate URL: %s\n", key)
				continue
			}
			seen[key] = true
			photoCount++
			localOpts = append(localOpts, MediaOption{
				Quality: fmt.Sprintf("Foto %d - Resolusi Penuh", photoCount),
				Format:  "jpg",
				Size:    "Dinamis",
				URL:     imgURL,
			})
		}
		if len(localOpts) > 0 {
			title = extractFirstMeta(html, "og:title", "twitter:title")
			if title == "" {
				title = "Instagram Post"
			}
			thumbnail = localOpts[0].URL
			log.Printf("[IG] %s berhasil: %d foto\n", viewer.name, len(localOpts))
			return title, thumbnail, localOpts, nil
		}
	}
	return "", "", nil, fmt.Errorf("all public viewers failed")
}

// ─── Method 6: oEmbed ────────────────────────────────────────────────────────

func fetchIGViaOEmbed(pageURL string) (title string, thumbnail string, options []MediaOption, err error) {
	apiURL := fmt.Sprintf("https://api.instagram.com/oembed/?url=%s&maxwidth=1080", pageURL)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", "", nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := igHTTPClient().Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", nil, fmt.Errorf("oEmbed status %d", resp.StatusCode)
	}
	var result struct {
		Title        string `json:"title"`
		AuthorName   string `json:"author_name"`
		ThumbnailURL string `json:"thumbnail_url"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", nil, err
	}
	if result.ThumbnailURL == "" {
		return "", "", nil, fmt.Errorf("no thumbnail from oEmbed")
	}
	title = result.Title
	if title == "" {
		title = fmt.Sprintf("Instagram Post @%s", result.AuthorName)
	}
	thumbnail = result.ThumbnailURL
	options = append(options, MediaOption{Quality: "Foto Resolusi Penuh", Format: "jpg", Size: "Dinamis", URL: result.ThumbnailURL})
	return title, thumbnail, options, nil
}

// ─── Main Instagram engine ────────────────────────────────────────────────────

func fetchInstagramImages(pageURL string) (title string, thumbnail string, options []MediaOption) {
	shortcode := extractIGShortcode(pageURL)
	if shortcode == "" {
		log.Printf("[IG] Tidak bisa ekstrak shortcode dari: %s\n", pageURL)
		return "", "", nil
	}
	log.Printf("[IG] Shortcode: %s\n", shortcode)

	type method struct {
		name string
		fn   func() (string, string, []MediaOption, error)
	}

	methods := []method{
		{"GraphQL API", func() (string, string, []MediaOption, error) { return fetchIGViaGraphQL(shortcode) }},
		{"Mobile API", func() (string, string, []MediaOption, error) { return fetchIGViaMobileAPI(shortcode) }},
		{"Embed JSON", func() (string, string, []MediaOption, error) { return fetchIGViaEmbedJSON(shortcode) }},
		{"Embed Scraping", func() (string, string, []MediaOption, error) { return fetchIGViaEmbed(shortcode) }},
		{"Public Viewer", func() (string, string, []MediaOption, error) { return fetchIGViaPublicViewer(shortcode) }},
		{"Downloadgram", func() (string, string, []MediaOption, error) {
			t, opts := fetchDownloadgramImages(pageURL)
			if len(opts) == 0 {
				return "", "", nil, fmt.Errorf("downloadgram returned 0 results")
			}
			return t, opts[0].URL, opts, nil
		}},
		{"oEmbed", func() (string, string, []MediaOption, error) { return fetchIGViaOEmbed(pageURL) }},
	}

	for _, m := range methods {
		log.Printf("[IG] Mencoba %s...\n", m.name)
		t, th, opts, err := m.fn()
		if err == nil && len(opts) > 0 {
			log.Printf("[IG] %s berhasil: %d foto (sebelum filter)\n", m.name, len(opts))
			// Apply final deduplication filter
			filteredOpts := filterImageOptions(opts)
			log.Printf("[IG] %s berhasil: %d foto (setelah filter)\n", m.name, len(filteredOpts))
			return t, th, filteredOpts
		}
		log.Printf("[IG] %s gagal: %v\n", m.name, err)
	}

	return "", "", nil
}
