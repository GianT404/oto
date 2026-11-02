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
	"github.com/mycophonic/saprobe-alac"
	"github.com/pion/opus/pkg/oggreader"
	"github.com/tphakala/go-m4a"
)

var adtsSampleRates = [...]int64{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}

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
	case ".aac":
		return adtsDuration(path)
	case ".m4a", ".mp4":
		if duration := m4aDuration(path); duration > 0 {
			return duration
		}
		return alacDuration(path)
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

func adtsDuration(path string) time.Duration {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0
	}
	const headerSize = 7
	var offset int64
	if info.Size() >= 10 {
		var id3 [10]byte
		if _, err := f.ReadAt(id3[:], 0); err == nil && string(id3[:3]) == "ID3" {
			for _, b := range id3[6:10] {
				if b&0x80 != 0 {
					return 0
				}
			}
			size := int64(id3[6])<<21 | int64(id3[7])<<14 | int64(id3[8])<<7 | int64(id3[9])
			offset = 10 + size
			if id3[5]&0x10 != 0 { // ID3v2.4 footer
				offset += 10
			}
			if offset > info.Size() {
				return 0
			}
		}
	}
	var duration time.Duration
	for offset+headerSize <= info.Size() {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return 0
		}
		var h [headerSize]byte
		if _, err := io.ReadFull(f, h[:]); err != nil {
			return 0
		}
		if h[0] != 0xff || h[1]&0xf6 != 0xf0 {
			return 0
		}
		rateIndex := int((h[2] >> 2) & 0x0f)
		if rateIndex >= len(adtsSampleRates) {
			return 0
		}
		frameLength := int64(h[3]&0x03)<<11 | int64(h[4])<<3 | int64(h[5]>>5)
		headerLen := int64(headerSize)
		if h[1]&1 == 0 { // CRC present
			headerLen += 2
		}
		if frameLength < headerLen || offset+frameLength > info.Size() {
			return 0
		}
		samples := 1024 * int64((h[6]&0x03)+1)
		duration += time.Duration(samples) * time.Second / time.Duration(adtsSampleRates[rateIndex])
		offset += frameLength
	}
	if offset != info.Size() || duration <= 0 {
		return 0
	}
	return duration
}

func m4aDuration(path string) time.Duration {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	reader, err := m4a.NewReader(f)
	if err != nil {
		return 0
	}
	return reader.Info().Duration
}

func alacDuration(path string) time.Duration {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	decoder, err := alac.NewDecoder(f)
	if err != nil {
		return 0
	}
	return decoder.Duration()
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
