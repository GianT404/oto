package player

import (
	"errors"
	"io"
	"math"
	"os"

	"github.com/cwbudde/aiff"
	"github.com/gopxl/beep/v2"
)

type aiffPCMStreamer struct {
	decoder *aiff.PCMReader
	format  aiff.PCMFormat
	buffer  []int32
	done    bool
	err     error
}

func openAIFFAudio(path string, startSec float64) (*beepAudioReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return decodeAIFFAudio(f, startSec)
}

func openAIFFAudioURL(rawURL string, headers map[string]string, startSec float64) (*beepAudioReader, error) {
	r, err := openHTTPRangeReader(rawURL, headers)
	if err != nil {
		return nil, err
	}
	return decodeAIFFAudio(r, startSec)
}

func decodeAIFFAudio(f readSeekCloser, startSec float64) (*beepAudioReader, error) {
	decoder, err := aiff.NewPCMReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	format := decoder.Format
	if format.Channels > 8 {
		f.Close()
		return nil, errors.New("unsupported AIFF channel count")
	}
	stream := beep.Resample(8, beep.SampleRate(format.SampleRate), beep.SampleRate(48000), &aiffPCMStreamer{
		decoder: decoder,
		format:  format,
		buffer:  make([]int32, 512*format.Channels),
	})
	if startSec > 0 {
		remaining := int(startSec * 48000)
		frames := make([][2]float64, 512)
		for remaining > 0 {
			n, ok := stream.Stream(frames[:min(remaining, len(frames))])
			remaining -= n
			if !ok || n == 0 {
				break
			}
		}
	}
	return &beepAudioReader{stream: stream, closer: f, frames: make([][2]float64, 512), gain: 1}, nil
}

func (s *aiffPCMStreamer) Stream(dst [][2]float64) (int, bool) {
	if s.done || len(dst) == 0 {
		return 0, !s.done
	}
	count := min(len(s.buffer), len(dst)*s.format.Channels)
	count -= count % s.format.Channels
	n, err := s.decoder.ReadPCM(s.buffer[:count])
	frames := n / s.format.Channels
	scale := math.Ldexp(1, s.format.BitDepth-1)
	for i := 0; i < frames; i++ {
		base := i * s.format.Channels
		left := float64(s.buffer[base]) / scale
		right := left
		if s.format.Channels > 1 {
			right = float64(s.buffer[base+1]) / scale
		}
		dst[i] = [2]float64{left, right}
	}
	if err != nil {
		s.done = true
		if err != io.EOF {
			s.err = err
		}
	}
	return frames, !s.done
}

func (s *aiffPCMStreamer) Err() error { return s.err }
