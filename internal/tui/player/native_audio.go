package player

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

func openNativeFile(path string, startSec float64) (io.ReadCloser, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".opus":
		return openOggOpus(path, time.Duration(startSec*float64(time.Second)))
	case ".mp3", ".wav", ".flac", ".ogg", ".oga":
		return openBeepAudio(path, startSec)
	case ".aac":
		return openAACAudio(path, startSec)
	case ".m4a", ".mp4":
		return openM4AAudio(path, startSec)
	case ".webm":
		return openWebMOpus(path, startSec)
	case ".aiff", ".aif":
		return openAIFFAudio(path, startSec)
	default:
		return nil, fmt.Errorf("unsupported audio format %q", filepath.Ext(path))
	}
}
