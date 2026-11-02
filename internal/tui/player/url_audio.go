package player

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"oto/internal/audio"
)

// openNativeURL recognizes URLs with audio containers supported by Go decoders.
func openNativeURL(rawURL string, headers map[string]string, startSec float64) (io.ReadCloser, bool, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	ext := strings.ToLower(filepath.Ext(req.URL.Path))
	if ext == ".m4a" || ext == ".mp4" {
		reader, err := openM4AAudioURL(rawURL, headers, startSec)
		return reader, err == nil, err
	}
	if ext == ".aiff" || ext == ".aif" {
		reader, err := openAIFFAudioURL(rawURL, headers, startSec)
		return reader, err == nil, err
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
		case "audio/webm", "video/webm":
			ext = ".webm"
		case "audio/mp4", "video/mp4", "application/mp4", "audio/x-m4a":
			ext = ".m4a"
		case "audio/aiff", "audio/x-aiff":
			ext = ".aiff"
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
	case ".m4a", ".mp4":
		_ = resp.Body.Close()
		reader, err = openM4AAudioURL(rawURL, headers, startSec)
	case ".aiff", ".aif":
		_ = resp.Body.Close()
		reader, err = openAIFFAudioURL(rawURL, headers, startSec)
	case ".webm":
		var stream io.ReadCloser
		stream, err = audio.OpenWebMOpusReader(resp.Body)
		if err == nil {
			reader, err = openOggOpusReader(stream, time.Duration(startSec*float64(time.Second)))
		}
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
