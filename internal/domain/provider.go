package domain

import "context"

// StreamInfo describes an audio URL and HTTP headers required by the CDN.
type StreamInfo struct {
	URL     string
	Headers map[string]string
}

type MusicProvider interface {
	Search(ctx context.Context, q string, offset int) ([]Track, error)
	Lyrics(ctx context.Context, id, title, artist string, duration int) (LyricsResp, error)
	ResolveStream(ctx context.Context, id string) (*StreamInfo, error)
	DownloadOPUS(ctx context.Context, id, title, destDir string) (string, error)
}


type LyricsSearchProvider interface {
	SearchLyrics(ctx context.Context, query string, limit int) ([]LyricsSearchResult, error)
}
