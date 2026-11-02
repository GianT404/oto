package player

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
	aacpcm "github.com/tphakala/go-aac/pcm"
	"github.com/tphakala/go-m4a/aacm4a"
)

type beepAudioReader struct {
	stream  beep.Streamer
	closer  io.Closer
	frames  [][2]float64
	pcm     []byte
	offset  int
	eof     atomic.Bool
	readErr atomic.Pointer[error]
	gain    float64
}

func openBeepAudio(path string, startSec float64) (*beepAudioReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return decodeBeepAudio(filepath.Ext(path), f, startSec, true)
}

func openAACAudio(path string, startSec float64) (*beepAudioReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return decodeAACAudio(f, startSec)
}

func decodeAACAudio(f io.ReadCloser, startSec float64) (*beepAudioReader, error) {
	decoder, err := aacpcm.NewDecoder(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	info := decoder.Info()
	if info.Channels != 1 && info.Channels != 2 {
		f.Close()
		return nil, errors.New("unsupported AAC channel count")
	}
	return newAACAudioReader(f, decoder, info.SampleRate, info.Channels, startSec)
}

func openM4AAudio(path string, startSec float64) (*beepAudioReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	decoder, info, err := aacm4a.NewDecoder(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Channels != 1 && info.Channels != 2 {
		f.Close()
		return nil, errors.New("unsupported M4A channel count")
	}
	if info.Duration < 0 {
		f.Close()
		return nil, errors.New("invalid M4A duration")
	}
	startSamples := int64(startSec * float64(info.SampleRate))
	totalSamples := int64(info.Duration.Seconds() * float64(info.SampleRate))
	if startSamples > totalSamples {
		startSamples = totalSamples
	}
	stride := int64(info.Channels * 2)
	limited := &aacM4APCMReader{
		source: decoder,
		skip:   (info.EncoderDelay + startSamples) * stride,
		limit:  (totalSamples - startSamples) * stride,
	}
	return newAACAudioReader(f, limited, info.SampleRate, info.Channels, 0)
}

func newAACAudioReader(f io.ReadCloser, decoder io.Reader, sampleRate, channels int, startSec float64) (*beepAudioReader, error) {
	stream := beep.Resample(8, beep.SampleRate(sampleRate), beep.SampleRate(48000), &aacPCMStreamer{
		decoder:  decoder,
		channels: channels,
		buffer:   make([]byte, 512*channels*2),
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

type aacM4APCMReader struct {
	source io.Reader
	skip   int64
	limit  int64
}

func (r *aacM4APCMReader) Read(dst []byte) (int, error) {
	if r.skip > 0 {
		n, err := io.CopyN(io.Discard, r.source, r.skip)
		r.skip -= n
		if err != nil {
			return 0, err
		}
	}
	if r.limit <= 0 {
		return 0, io.EOF
	}
	if int64(len(dst)) > r.limit {
		dst = dst[:int(r.limit)]
	}
	n, err := r.source.Read(dst)
	r.limit -= int64(n)
	if r.limit <= 0 && err == nil {
		return n, io.EOF
	}
	return n, err
}

type aacPCMStreamer struct {
	decoder  io.Reader
	channels int
	buffer   []byte
	done     bool
	err      error
}

func (s *aacPCMStreamer) Stream(dst [][2]float64) (int, bool) {
	if s.done || len(dst) == 0 {
		return 0, !s.done
	}
	stride := s.channels * 2
	need := min(len(dst)*stride, len(s.buffer))
	n, err := io.ReadFull(s.decoder, s.buffer[:need])
	frames := n / stride
	for i := 0; i < frames; i++ {
		left := float64(int16(binary.LittleEndian.Uint16(s.buffer[i*stride:]))) / 32768
		right := left
		if s.channels == 2 {
			right = float64(int16(binary.LittleEndian.Uint16(s.buffer[i*stride+2:]))) / 32768
		}
		dst[i] = [2]float64{left, right}
	}
	if err != nil {
		s.done = true
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			s.err = err
		}
	}
	return frames, !s.done
}

func (s *aacPCMStreamer) Err() error { return s.err }

func decodeBeepAudio(ext string, f io.ReadCloser, startSec float64, seekable bool) (*beepAudioReader, error) {
	var stream beep.StreamSeekCloser
	var format beep.Format
	var err error
	switch strings.ToLower(ext) {
	case ".mp3":
		stream, format, err = mp3.Decode(f)
	case ".flac":
		stream, format, err = flac.Decode(f)
	case ".wav":
		stream, format, err = wav.Decode(f)
	case ".ogg", ".oga":
		stream, format, err = vorbis.Decode(f)
	default:
		err = errors.New("unsupported audio format")
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	var decoded beep.Streamer = stream
	if startSec > 0 {
		if seekable {
			position := int(startSec * float64(format.SampleRate))
			if position > stream.Len() {
				position = stream.Len()
			}
			if err := stream.Seek(position); err != nil {
				stream.Close()
				return nil, err
			}
		} else {
			frames := make([][2]float64, 512)
			remaining := int(startSec * float64(format.SampleRate))
			for remaining > 0 {
				count := min(remaining, len(frames))
				n, ok := stream.Stream(frames[:count])
				remaining -= n
				if !ok || n == 0 {
					break
				}
			}
		}
	}
	decoded = beep.Resample(8, format.SampleRate, beep.SampleRate(48000), stream)
	return &beepAudioReader{stream: decoded, closer: stream, frames: make([][2]float64, 512), gain: 1}, nil
}

func (r *beepAudioReader) Read(dst []byte) (int, error) {
	if r.offset >= len(r.pcm) {
		n, ok := r.stream.Stream(r.frames)
		if n == 0 && !ok {
			r.eof.Store(true)
			if err := r.stream.Err(); err != nil {
				r.readErr.Store(&err)
				return 0, err
			}
			return 0, io.EOF
		}
		r.pcm = make([]byte, n*4)
		for i, frame := range r.frames[:n] {
			left := int16(max(-1, min(1, frame[0])) * 32767)
			right := int16(max(-1, min(1, frame[1])) * 32767)
			binary.LittleEndian.PutUint16(r.pcm[i*4:], uint16(left))
			binary.LittleEndian.PutUint16(r.pcm[i*4+2:], uint16(right))
		}
		normalizePCM16(r.pcm, &r.gain)
		r.offset = 0
	}
	n := copy(dst, r.pcm[r.offset:])
	r.offset += n
	return n, nil
}

func (r *beepAudioReader) Close() error { return r.closer.Close() }

func (r *beepAudioReader) finished() bool { return r.eof.Load() }

// normalizePCM16 applies block peak normalization, the native equivalent of the
// player's default dynaudnorm preset. Samples are interleaved stereo S16LE.
func normalizePCM16(pcm []byte, currentGain *float64) {
	var peak int32
	for i := 0; i+1 < len(pcm); i += 2 {
		sample := int32(int16(binary.LittleEndian.Uint16(pcm[i:])))
		if sample < 0 {
			sample = -sample
		}
		if sample > peak {
			peak = sample
		}
	}
	if peak == 0 || peak < 256 {
		return
	}
	targetGain := float64(29490) / float64(peak) // target peak: 90% of full scale
	if targetGain > 4 {
		targetGain = 4
	}
	if targetGain < 0.25 {
		targetGain = 0.25
	}
	*currentGain = *currentGain*0.8 + targetGain*0.2
	for i := 0; i+1 < len(pcm); i += 2 {
		sample := int32(int16(binary.LittleEndian.Uint16(pcm[i:])))
		sample = int32(float64(sample) * *currentGain)
		if sample > 32767 {
			sample = 32767
		} else if sample < -32768 {
			sample = -32768
		}
		binary.LittleEndian.PutUint16(pcm[i:], uint16(int16(sample)))
	}
}
