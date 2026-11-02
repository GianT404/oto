package player

import (
	"io"
	"sync/atomic"
)

type playbackErrorSource interface {
	playbackError() error
}

func playbackErrorFrom(reader io.ReadCloser) error {
	if source, ok := reader.(playbackErrorSource); ok {
		return source.playbackError()
	}
	return nil
}

func storePlaybackError(dst *atomic.Pointer[error], err error) {
	if err == nil {
		return
	}
	copyErr := err
	dst.Store(&copyErr)
}
