package main

import (
	"testing"
)

func TestFetchDownloadgramInstagramPost(t *testing.T) {
	title, opts := fetchDownloadgramImages("https://www.instagram.com/p/DYoU6beEiTf/?utm_source=ig_web")
	if len(opts) == 0 {
		t.Fatal("expected image options from downloadgram")
	}
	t.Logf("title=%q options=%d first=%s", title, len(opts), opts[0].URL[:80])
}

func TestFetchInstagramEngine(t *testing.T) {
	// Test dengan link carousel Instagram publik
	testURL := "https://www.instagram.com/p/DYoufTFk_QS/"
	title, thumbnail, opts := fetchInstagramImages(testURL)
	t.Logf("title=%q thumbnail=%q options=%d", title, thumbnail, len(opts))
	for i, o := range opts {
		t.Logf("  #%d: %s", i+1, o.URL)
	}
	if len(opts) == 0 {
		t.Log("WARNING: No images found - Instagram may be blocking requests")
	}
}

func TestDownloadgramCount(t *testing.T) {
	// Test berapa foto yang dikembalikan downloadgram untuk URL dari screenshot
	testURL := "https://www.instagram.com/p/DYoufTFk_QS/"
	title, opts := fetchDownloadgramImages(testURL)
	t.Logf("Downloadgram: title=%q options=%d", title, len(opts))
	for i, o := range opts {
		t.Logf("  #%d url=%s", i+1, o.URL)
	}
}
