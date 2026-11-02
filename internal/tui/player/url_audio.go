package player

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// openNativeURL recognizes URLs with supported audio containers. Unsupported
// containers are left to the FFmpeg fallback.
func openNativeURL(rawURL string, headers map[string]string, startSec float64) (io.ReadCloser, bool, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, false, nil
	}
	ext := strings.ToLower(filepath.Ext(req.URL.Path))
	if ext == "" {
		mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		switch mediaType {
		case "audio/ogg", "application/ogg":
			ext = ".ogg"
		case "audio/mpeg":
			ext = ".mp3"
		case "audio/flac":
			ext = ".flac"
		case "audio/wav", "audio/x-wav":
			ext = ".wav"
		case "audio/aac", "audio/aacp":
			ext = ".aac"
		}
	}
	var reader io.ReadCloser
	switch ext {
	case ".opus":
		reader, err = openOggOpusReader(resp.Body, time.Duration(startSec*float64(time.Second)))
	case ".mp3", ".wav", ".flac", ".ogg", ".oga":
		reader, err = decodeBeepAudio(ext, resp.Body, startSec, false)
	case ".aac":
		reader, err = decodeAACAudio(resp.Body, startSec)
	default:
		resp.Body.Close()
		return nil, false, nil
	}
	if err != nil {
		resp.Body.Close()
		return nil, false, err
	}
	return reader, true, nil
}
