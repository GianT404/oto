package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const youtubeSearchAPIKey = "AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8"

var youtubeSearchDialer = &net.Dialer{Timeout: 5 * time.Second}

var youtubeSearchClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return youtubeSearchDialer.DialContext(ctx, "tcp4", addr)
		},
		ForceAttemptHTTP2: true,
	},
}

func searchYouTube(ctx context.Context, keyword string, offset int) ([]SearchResult, error) {
	body, err := json.Marshal(map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "WEB",
				"clientVersion": "2.20250312.04.00",
				"hl":            "en",
				"gl":            "US",
			},
		},
		"query": keyword,
	})
	if err != nil {
		return nil, err
	}

	endpoint := "https://www.youtube.com/youtubei/v1/search?key=" + youtubeSearchAPIKey + "&prettyPrint=false"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", defaultUA)

	resp, err := youtubeSearchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube search: HTTP %d", resp.StatusCode)
	}

	var payload any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}

	return parseYouTubeSearch(payload, offset), nil
}

func parseYouTubeSearch(payload any, offset int) []SearchResult {
	renderers := collectVideoRenderers(payload)
	results := make([]SearchResult, 0, len(renderers))
	for _, vr := range renderers {
		id, _ := vr["videoId"].(string)
		if len(id) != 11 || strings.HasPrefix(id, "@") {
			continue
		}
		results = append(results, SearchResult{
			ID:        id,
			Title:     youtubeText(vr["title"]),
			Thumbnail: youtubeThumbnail(vr),
			Duration:  youtubeDuration(vr),
			Uploader:  youtubeUploader(vr),
			Artist:    youtubeUploader(vr),
		})
	}

	if offset >= len(results) {
		return []SearchResult{}
	}
	end := offset + SearchPageSize
	if end > len(results) {
		end = len(results)
	}
	return results[offset:end]
}

func collectVideoRenderers(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if vr, ok := n["videoRenderer"].(map[string]any); ok {
				out = append(out, vr)
				break
			}
			for _, child := range n {
				walk(child)
			}
		case []any:
			for _, child := range n {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

func youtubeText(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	if runs, ok := m["runs"].([]any); ok {
		var b strings.Builder
		for _, r := range runs {
			if rm, ok := r.(map[string]any); ok {
				if s, ok := rm["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return b.String()
	}
	s, _ := m["simpleText"].(string)
	return s
}

func youtubeDuration(vr map[string]any) int {
	lt, ok := vr["lengthText"].(map[string]any)
	if !ok {
		return 0
	}
	parts := strings.Split(youtubeText(lt), ":")
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0
		}
		total = total*60 + n
	}
	return total
}

func youtubeThumbnail(vr map[string]any) string {
	th, ok := vr["thumbnail"].(map[string]any)
	if !ok {
		return ""
	}
	thumbs, ok := th["thumbnails"].([]any)
	if !ok || len(thumbs) == 0 {
		return ""
	}
	last, _ := thumbs[len(thumbs)-1].(map[string]any)
	u, _ := last["url"].(string)
	return u
}

func youtubeUploader(vr map[string]any) string {
	for _, key := range []string{"ownerText", "longBylineText", "shortBylineText"} {
		if s := youtubeText(vr[key]); s != "" {
			return s
		}
	}
	return ""
}
