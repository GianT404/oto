package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var reYouTubeAPIKey = regexp.MustCompile(`"INNERTUBE_API_KEY":"([^"]+)"`)

var (
	youtubeAPIKeyMu    sync.Mutex
	youtubeAPIKeyCache string
	youtubeAPIKeyUntil time.Time
)

func parseYouTubeAPIKey(body []byte) string {
	match := reYouTubeAPIKey.FindSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return string(match[1])
}

func getYouTubeAPIKey(ctx context.Context) (string, error) {
	if key := strings.TrimSpace(os.Getenv("OTO_YOUTUBE_API_KEY")); key != "" {
		return key, nil
	}

	youtubeAPIKeyMu.Lock()
	defer youtubeAPIKeyMu.Unlock()
	if youtubeAPIKeyCache != "" && time.Now().Before(youtubeAPIKeyUntil) {
		return youtubeAPIKeyCache, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.youtube.com/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultUA)
	resp, err := youtubeSearchClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("youtube config: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	key := parseYouTubeAPIKey(body)
	if key == "" {
		return "", fmt.Errorf("youtube config: missing INNERTUBE_API_KEY")
	}
	youtubeAPIKeyCache = key
	youtubeAPIKeyUntil = time.Now().Add(24 * time.Hour)
	return key, nil
}

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

	key, err := getYouTubeAPIKey(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := "https://www.youtube.com/youtubei/v1/search?key=" + url.QueryEscape(key) + "&prettyPrint=false"
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
