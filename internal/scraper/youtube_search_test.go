package scraper

import (
	"encoding/json"
	"testing"
)

func TestParseYouTubeSearch(t *testing.T) {
	const raw = `{"contents":{"twoColumnSearchResultsRenderer":{"primaryContents":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[
		{"videoRenderer":{"videoId":"dQw4w9WgXcQ","title":{"runs":[{"text":"Never Gonna Give You Up"}]},"lengthText":{"simpleText":"3:33"},"ownerText":{"runs":[{"text":"Rick Astley"}]},"thumbnail":{"thumbnails":[{"url":"small.jpg"},{"url":"big.jpg"}]}}},
		{"channelRenderer":{"channelId":"UC123"}},
		{"videoRenderer":{"videoId":"abcdefghijk","title":{"simpleText":"Second"},"lengthText":{"simpleText":"1:02:03"},"longBylineText":{"runs":[{"text":"Artist B"}]}}}
	]}}]}}}}}`
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}

	got := parseYouTubeSearch(payload, 0)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if got[0].ID != "dQw4w9WgXcQ" || got[0].Duration != 213 || got[0].Artist != "Rick Astley" || got[0].Thumbnail != "big.jpg" {
		t.Fatalf("unexpected first result: %+v", got[0])
	}
	if got[1].Duration != 3723 || got[1].Artist != "Artist B" {
		t.Fatalf("unexpected second result: %+v", got[1])
	}
	if page := parseYouTubeSearch(payload, 1); len(page) != 1 || page[0].ID != "abcdefghijk" {
		t.Fatalf("offset page: %+v", page)
	}
}
