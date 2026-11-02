package player

import (
	"io"
	"sync/atomic"
)

// speedPCMReader changes playback rate by dropping or repeating stereo PCM
// frames. This intentionally changes pitch, matching the player's Nightcore /
// Daycore behavior while avoiding an external transcoder.
type speedPCMReader struct {
	source  io.ReadCloser
	speed   float64
	phase   float64
	current [4]byte
	loaded  bool
	done    bool
	eof     atomic.Bool
	err     atomic.Pointer[error]
}

func withPlaybackSpeed(source io.ReadCloser, speed float64) io.ReadCloser {
	if speed == 1 {
		return source
	}
	return &speedPCMReader{source: source, speed: speed}
}

func (r *speedPCMReader) Read(dst []byte) (int, error) {
	frames := len(dst) / 4
	if frames == 0 {
		return 0, io.ErrShortBuffer
	}
	written := 0
	for written < frames {
		if !r.loaded {
			if r.done {
				break
			}
			if _, err := io.ReadFull(r.source, r.current[:]); err != nil {
				r.done = true
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					break
				}
				storePlaybackError(&r.err, err)
				if written == 0 {
					return 0, err
				}
				break
			}
			r.loaded = true
		}
		copy(dst[written*4:], r.current[:])
		written++
		r.phase += r.speed
		for r.phase >= 1 {
			r.phase--
			r.loaded = false
			if _, err := io.ReadFull(r.source, r.current[:]); err != nil {
				r.done = true
				if err != io.EOF && err != io.ErrUnexpectedEOF {
					storePlaybackError(&r.err, err)
				}
				if err != io.EOF && err != io.ErrUnexpectedEOF && written == 0 {
					return 0, err
				}
				break
			}
			r.loaded = true
		}
	}
	if r.done && !r.loaded {
		r.eof.Store(true)
	}
	if written == 0 {
		return 0, io.EOF
	}
	return written * 4, nil
}

func (r *speedPCMReader) Close() error   { return r.source.Close() }
func (r *speedPCMReader) finished() bool { return r.eof.Load() }

func (r *speedPCMReader) playbackError() error {
	if err := r.err.Load(); err != nil {
		return *err
	}
	return playbackErrorFrom(r.source)
}
