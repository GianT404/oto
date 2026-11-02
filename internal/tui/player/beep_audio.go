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
)

type beepAudioReader struct {
	stream  beep.Streamer
	closer  io.Closer
	frames  [][2]float64
	pcm     []byte
	offset  int
	eof     atomic.Bool
	readErr atomic.Pointer[error]
}

func openBeepAudio(path string, startSec float64) (*beepAudioReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var stream beep.StreamSeekCloser
	var format beep.Format
	switch strings.ToLower(filepath.Ext(path)) {
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
	decoded = beep.Resample(8, format.SampleRate, beep.SampleRate(48000), decoded)
	if startSec > 0 {
		position := int(startSec * float64(format.SampleRate))
		if position > stream.Len() {
			position = stream.Len()
		}
		if err := stream.Seek(position); err != nil {
			stream.Close()
			return nil, err
		}
		decoded = beep.Resample(8, format.SampleRate, beep.SampleRate(48000), stream)
	}
	return &beepAudioReader{stream: decoded, closer: stream, frames: make([][2]float64, 512)}, nil
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
		r.offset = 0
	}
	n := copy(dst, r.pcm[r.offset:])
	r.offset += n
	return n, nil
}

func (r *beepAudioReader) Close() error { return r.closer.Close() }

func (r *beepAudioReader) finished() bool { return r.eof.Load() }
