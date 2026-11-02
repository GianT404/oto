package player

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/pion/opus"
	"github.com/pion/opus/pkg/oggreader"
)

// oggOpusReader decodes an Ogg Opus file to the 48 kHz stereo PCM expected by oto.
type oggOpusReader struct {
	file      io.ReadCloser
	ogg       *oggreader.OggReader
	decoder   opus.Decoder
	channels  int
	preskip   int
	pcm       []byte
	pcmOffset int
	closed    bool
	eof       atomic.Bool
	readErr   atomic.Pointer[opusReadError]
	gain      float64
}

type opusReadError struct{ err error }

func openOggOpus(path string, start time.Duration) (*oggOpusReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return openOggOpusReader(f, start)
}

func openOggOpusReader(f io.ReadCloser, start time.Duration) (*oggOpusReader, error) {
	ogg, head, err := oggreader.NewWith(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	channels := int(head.Channels)
	if channels != 1 && channels != 2 {
		f.Close()
		return nil, errors.New("unsupported Opus channel count")
	}
	dec, err := opus.NewDecoderWithOutput(48000, channels)
	if err != nil {
		f.Close()
		return nil, err
	}
	r := &oggOpusReader{file: f, ogg: ogg, decoder: dec, channels: channels, preskip: int(head.PreSkip), gain: 1}
	// The first packet after OpusHead is the OpusTags metadata packet.
	if _, _, err = ogg.ParseNextPacket(); err != nil {
		r.Close()
		return nil, err
	}
	if start > 0 {
		seekBytes := int64(start.Seconds() * 48000 * 4)
		seekBytes -= seekBytes % 4 // keep the PCM cursor aligned to stereo frames
		if _, err := io.CopyN(io.Discard, r, seekBytes); err != nil && err != io.EOF {
			r.Close()
			return nil, err
		}
	}
	return r, nil
}

func (r *oggOpusReader) Read(dst []byte) (int, error) {
	for r.pcmOffset >= len(r.pcm) {
		packet, _, err := r.ogg.ParseNextPacket()
		if err != nil {
			r.eof.Store(true)
			if err != io.EOF {
				r.readErr.Store(&opusReadError{err: err})
			}
			return 0, err
		}
		// Opus packets are at most 120 ms at 48 kHz.
		decoded := make([]int16, 5760*r.channels)
		n, err := r.decoder.DecodeToInt16(packet, decoded)
		if err != nil {
			r.eof.Store(true)
			r.readErr.Store(&opusReadError{err: err})
			return 0, err
		}
		if r.preskip > 0 {
			drop := min(r.preskip, n)
			decoded = decoded[drop*r.channels:]
			n -= drop
			r.preskip -= drop
		}
		pcm := make([]byte, n*4)
		if r.channels == 2 {
			for i, sample := range decoded[:n*2] {
				binary.LittleEndian.PutUint16(pcm[i*2:], uint16(sample))
			}
		} else {
			for i, sample := range decoded[:n] {
				binary.LittleEndian.PutUint16(pcm[i*4:], uint16(sample))
				binary.LittleEndian.PutUint16(pcm[i*4+2:], uint16(sample))
			}
		}
		normalizePCM16(pcm, &r.gain)
		r.pcm, r.pcmOffset = pcm, 0
		if len(pcm) == 0 {
			continue
		}
	}
	n := copy(dst, r.pcm[r.pcmOffset:])
	r.pcmOffset += n
	return n, nil
}

func (r *oggOpusReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.file.Close()
}

func (r *oggOpusReader) playbackError() error {
	if err := r.readErr.Load(); err != nil {
		return err.err
	}
	return nil
}
