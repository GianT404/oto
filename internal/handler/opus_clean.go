package handler

import (
	"context"
	"errors"
	"io"

	"github.com/thesyncim/gopus"
	"github.com/thesyncim/gopus/container/ogg"
)

// cleanOpus decodes an Ogg Opus stream, removes sustained silence, and writes
// an Ogg Opus stream without invoking an external media process.
func cleanOpus(ctx context.Context, src io.Reader, dst io.Writer) (retErr error) {
	reader, err := ogg.NewReader(src)
	if err != nil {
		return err
	}
	channels := int(reader.Header.Channels)
	if channels != 1 && channels != 2 {
		return errors.New("unsupported Opus channel count")
	}
	decoder, err := gopus.NewDecoder(gopus.DecoderConfig{SampleRate: 48000, Channels: channels})
	if err != nil {
		return err
	}
	encoder, err := gopus.NewEncoder(gopus.EncoderConfig{SampleRate: 48000, Channels: 2, Application: gopus.ApplicationAudio})
	if err != nil {
		return err
	}
	if err := encoder.SetBitrate(160000); err != nil {
		return err
	}
	writer, err := ogg.NewWriter(dst, 48000, 2)
	if err != nil {
		return err
	}
	defer func() {
		if err := writer.Close(); retErr == nil {
			retErr = err
		}
	}()

	const frameSize = 960
	const silenceLimit = 24000
	const threshold = 104
	decoded := make([]int16, 5760*channels)
	pending := make([]int16, 0, frameSize*2)
	quiet := make([]int16, 0, silenceLimit*2)
	packetBuf := make([]byte, 4000)
	preSkip := int(reader.Header.PreSkip)
	started := false

	writeSamples := func(samples []int16) error {
		pending = append(pending, samples...)
		for len(pending) >= frameSize*2 {
			n, err := encoder.EncodeInt16(pending[:frameSize*2], packetBuf)
			if err != nil {
				return err
			}
			if err := writer.WritePacket(packetBuf[:n], frameSize); err != nil {
				return err
			}
			pending = pending[frameSize*2:]
		}
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		packet, _, err := reader.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		n, err := decoder.DecodeInt16(packet, decoded)
		if err != nil {
			return err
		}
		if preSkip > 0 {
			drop := min(preSkip, n)
			decoded = decoded[drop*channels:]
			n -= drop
			preSkip -= drop
		}
		stereo := make([]int16, n*2)
		for i := 0; i < n; i++ {
			if channels == 1 {
				stereo[i*2], stereo[i*2+1] = decoded[i], decoded[i]
			} else {
				stereo[i*2], stereo[i*2+1] = decoded[i*2], decoded[i*2+1]
			}
		}
		decoded = make([]int16, 5760*channels)

		for frame := 0; frame < n; frame++ {
			left, right := stereo[frame*2], stereo[frame*2+1]
			isQuiet := absSample(left) <= threshold && absSample(right) <= threshold
			if isQuiet {
				quiet = append(quiet, left, right)
				if len(quiet) >= silenceLimit*2 {
					quiet = quiet[:0]
				}
				continue
			}
			if !started {
				started = true
			} else if len(quiet) > 0 {
				if err := writeSamples(quiet); err != nil {
					return err
				}
			}
			quiet = quiet[:0]
			if err := writeSamples(stereo[frame*2 : frame*2+2]); err != nil {
				return err
			}
		}
	}
	if len(quiet) > 0 && len(quiet) < silenceLimit*2 {
		if err := writeSamples(quiet); err != nil {
			return err
		}
	}
	if len(pending) > 0 {
		actualSamples := len(pending) / 2
		padded := make([]int16, frameSize*2)
		copy(padded, pending)
		n, err := encoder.EncodeInt16(padded, packetBuf)
		if err != nil {
			return err
		}
		if err := writer.WritePacket(packetBuf[:n], actualSamples); err != nil {
			return err
		}
	}
	return nil
}

func absSample(v int16) int32 {
	n := int32(v)
	if n < 0 {
		return -n
	}
	return n
}
