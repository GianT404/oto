package player

import (
	"encoding/binary"
	"io"
	"sync/atomic"
)

// silencePCMReader drops sustained low-level PCM while retaining short pauses.
type silencePCMReader struct {
	source        io.ReadCloser
	silenceFrames int
	pending       []byte
	eof           atomic.Bool
	err           atomic.Pointer[error]
}

func skipSilence(source io.ReadCloser) io.ReadCloser {
	return &silencePCMReader{source: source}
}

func (r *silencePCMReader) Read(dst []byte) (int, error) {
	for len(r.pending) == 0 {
		buf := make([]byte, 4096)
		n, err := r.source.Read(buf)
		if n > 0 {
			out := make([]byte, 0, n)
			for i := 0; i+3 < n; i += 4 {
				left := int32(int16(binary.LittleEndian.Uint16(buf[i:])))
				right := int32(int16(binary.LittleEndian.Uint16(buf[i+2:])))
				if abs32(left) <= 104 && abs32(right) <= 104 {
					r.silenceFrames++
					if r.silenceFrames > 24000 { // 0.5 seconds at 48 kHz
						continue
					}
				} else {
					r.silenceFrames = 0
				}
				out = append(out, buf[i:i+4]...)
			}
			r.pending = out
		}
		if err != nil {
			r.eof.Store(true)
			if err != io.EOF {
				storePlaybackError(&r.err, err)
			}
			if len(r.pending) == 0 {
				return 0, err
			}
		}
	}
	n := copy(dst, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *silencePCMReader) Close() error   { return r.source.Close() }
func (r *silencePCMReader) finished() bool { return r.eof.Load() }

func (r *silencePCMReader) playbackError() error {
	if err := r.err.Load(); err != nil {
		return *err
	}
	return playbackErrorFrom(r.source)
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

var _ io.ReadCloser = (*silencePCMReader)(nil)
