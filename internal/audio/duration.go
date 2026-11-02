package audio

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cwbudde/aiff"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
	"github.com/pion/opus/pkg/oggreader"
)

// FileDuration probes supported audio containers using their native Go readers.
// It returns zero when the file is malformed or its duration is unavailable.
func FileDuration(path string) time.Duration {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return durationFromBeep(path, func(r io.ReadCloser) (beep.StreamSeekCloser, beep.Format, error) { return mp3.Decode(r) })
	case ".wav":
		return durationFromBeep(path, func(r io.ReadCloser) (beep.StreamSeekCloser, beep.Format, error) { return wav.Decode(r) })
	case ".flac":
		return durationFromBeep(path, func(r io.ReadCloser) (beep.StreamSeekCloser, beep.Format, error) { return flac.Decode(r) })
	case ".ogg", ".oga":
		if duration := durationFromBeep(path, func(r io.ReadCloser) (beep.StreamSeekCloser, beep.Format, error) { return vorbis.Decode(r) }); duration > 0 {
			return duration
		}
		return oggOpusDuration(path)
	case ".opus":
		return oggOpusDuration(path)
	case ".aiff", ".aif":
		return aiffDuration(path)
	case ".webm":
		duration, err := WebMOpusDuration(path)
		if err == nil {
			return duration
		}
	}
	return 0
}

type beepDecoder func(io.ReadCloser) (beep.StreamSeekCloser, beep.Format, error)

func durationFromBeep(path string, decode beepDecoder) time.Duration {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	stream, format, err := decode(f)
	if err != nil {
		_ = f.Close()
		return 0
	}
	defer stream.Close()
	if format.SampleRate <= 0 || stream.Len() <= 0 {
		return 0
	}
	return time.Duration(stream.Len()) * time.Second / time.Duration(format.SampleRate)
}

func oggOpusDuration(path string) time.Duration {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	reader, head, err := oggreader.NewWith(f)
	if err != nil {
		return 0
	}
	var granule uint64
	for {
		_, page, err := reader.ParseNextPage()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0
		}
		if page.GranulePosition != ^uint64(0) {
			granule = page.GranulePosition
		}
	}
	if granule <= uint64(head.PreSkip) {
		return 0
	}
	return time.Duration(granule-uint64(head.PreSkip)) * time.Second / 48000
}

func aiffDuration(path string) time.Duration {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	decoder := aiff.NewDecoder(f)
	duration, err := decoder.Duration()
	if err != nil {
		return 0
	}
	return duration
}
