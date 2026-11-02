package player

import (
	"time"

	"oto/internal/audio"
)

func openWebMOpus(path string, startSec float64) (*oggOpusReader, error) {
	stream, err := audio.OpenWebMOpus(path)
	if err != nil {
		return nil, err
	}
	return openOggOpusReader(stream, time.Duration(startSec*float64(time.Second)))
}
