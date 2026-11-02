package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/5rahim/gomkv"
	"github.com/thesyncim/gopus/container/ogg"
)

// RemuxWebMOpus copies Opus packets from a WebM audio track into an Ogg Opus
// stream without decoding or re-encoding them.
func RemuxWebMOpus(srcPath, dstPath string) (err error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	track, err := discoverWebMOpusTrack(src)
	if err != nil {
		return err
	}

	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return err
	}
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dst.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	writer, err := newWebMOggWriter(dst, track)
	if err != nil {
		return fmt.Errorf("create Ogg Opus writer: %w", err)
	}
	blocks := &webmOpusBlocks{track: track.number, writer: writer}
	if err := gomkv.ParseSections(src, blocks, gomkv.ClusterElement); err != nil {
		return fmt.Errorf("parse WebM audio blocks: %w", err)
	}
	if blocks.packets == 0 {
		return errors.New("WebM Opus track contains no audio packets")
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish Ogg Opus stream: %w", err)
	}
	return nil
}

// OpenWebMOpus returns an Ogg Opus stream backed by a WebM demux goroutine.
// The input is decoded packet-by-packet without first materializing a temp file.
func OpenWebMOpus(path string) (io.ReadCloser, error) {
	src, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return OpenWebMOpusReader(src)
}

// OpenWebMOpusReader demuxes a WebM Opus stream from any sequential reader.
// It is suitable for HTTP response bodies as well as local files.
func OpenWebMOpusReader(src io.ReadCloser) (io.ReadCloser, error) {
	if src == nil {
		return nil, errors.New("nil WebM Opus source")
	}
	reader, writer := io.Pipe()
	stream := &webmOpusPipe{PipeReader: reader, src: src}
	go func() {
		demux := &webmOpusDemux{output: writer}
		parseErr := gomkv.Parse(src, demux)
		if parseErr == nil && demux.blocks.packets == 0 {
			parseErr = errors.New("WebM Opus track contains no audio packets")
		}
		if parseErr == nil {
			parseErr = demux.blocks.writer.Close()
		}
		_ = src.Close()
		if parseErr != nil {
			_ = writer.CloseWithError(fmt.Errorf("stream WebM Opus: %w", parseErr))
		} else {
			_ = writer.Close()
		}
	}()
	return stream, nil
}

func newWebMOggWriter(w io.Writer, track *webmOpusTrack) (*ogg.Writer, error) {
	return ogg.NewWriterWithConfig(w, ogg.WriterConfig{
		SampleRate:    48000,
		Channels:      uint8(track.channels),
		PreSkip:       track.preSkip,
		OutputGain:    track.outputGain,
		MappingFamily: 0,
	})
}

type webmOpusPipe struct {
	*io.PipeReader
	src  io.ReadCloser
	once sync.Once
}

type webmOpusDemux struct {
	gomkv.DefaultHandler
	track  webmOpusTrack
	blocks webmOpusBlocks
	output io.Writer
}

func (h *webmOpusDemux) HandleMasterBegin(id gomkv.ElementID, info gomkv.ElementInfo) (bool, error) {
	return h.track.HandleMasterBegin(id, info)
}

func (h *webmOpusDemux) HandleMasterEnd(id gomkv.ElementID, info gomkv.ElementInfo) error {
	if err := h.track.HandleMasterEnd(id, info); err != nil {
		return err
	}
	if id == gomkv.TrackEntryElement && h.blocks.writer == nil && h.track.codec == "A_OPUS" {
		if err := validateWebMOpusTrack(&h.track); err != nil {
			return err
		}
		writer, err := newWebMOggWriter(h.output, &h.track)
		if err != nil {
			return err
		}
		h.blocks = webmOpusBlocks{track: h.track.number, writer: writer}
	}
	return nil
}

func (h *webmOpusDemux) HandleInteger(id gomkv.ElementID, value int64, info gomkv.ElementInfo) error {
	return h.track.HandleInteger(id, value, info)
}

func (h *webmOpusDemux) HandleString(id gomkv.ElementID, value string, info gomkv.ElementInfo) error {
	return h.track.HandleString(id, value, info)
}

func (h *webmOpusDemux) HandleBinary(id gomkv.ElementID, value []byte, info gomkv.ElementInfo) error {
	if id == gomkv.CodecPrivateElement && h.track.inEntry {
		return h.track.HandleBinary(id, value, info)
	}
	if h.blocks.writer != nil {
		return h.blocks.HandleBinary(id, value, info)
	}
	return nil
}

func (r *webmOpusPipe) Close() error {
	var err error
	r.once.Do(func() {
		err = r.PipeReader.Close()
		_ = r.src.Close()
	})
	return err
}

func discoverWebMOpusTrack(src io.ReadSeeker) (*webmOpusTrack, error) {
	track := &webmOpusTrack{}
	if err := gomkv.ParseSections(src, track, gomkv.TracksElement); err != nil {
		return nil, fmt.Errorf("parse WebM tracks: %w", err)
	}
	if track.number == 0 || track.codec != "A_OPUS" {
		return nil, errors.New("WebM does not contain an Opus audio track")
	}
	if err := validateWebMOpusTrack(track); err != nil {
		return nil, err
	}
	return track, nil
}

func validateWebMOpusTrack(track *webmOpusTrack) error {
	if track.channels != 1 && track.channels != 2 {
		return fmt.Errorf("unsupported WebM Opus channel count: %d", track.channels)
	}
	if len(track.codecPrivate) < 19 || string(track.codecPrivate[:8]) != "OpusHead" {
		return errors.New("WebM Opus track is missing a valid OpusHead")
	}
	if track.codecPrivate[18] != 0 {
		return errors.New("unsupported WebM Opus channel mapping")
	}
	track.preSkip = binary.LittleEndian.Uint16(track.codecPrivate[10:12])
	track.outputGain = int16(binary.LittleEndian.Uint16(track.codecPrivate[16:18]))
	return nil
}

type webmOpusTrack struct {
	gomkv.DefaultHandler
	inEntry      bool
	entryNumber  uint64
	entryCodec   string
	entryChans   int
	entryPrivate []byte
	number       uint64
	codec        string
	channels     int
	codecPrivate []byte
	preSkip      uint16
	outputGain   int16
}

func (h *webmOpusTrack) HandleMasterBegin(id gomkv.ElementID, info gomkv.ElementInfo) (bool, error) {
	if id == gomkv.TrackEntryElement {
		h.inEntry = true
		h.entryNumber, h.entryCodec, h.entryChans = 0, "", 0
		h.entryPrivate = nil
	}
	return true, nil
}

func (h *webmOpusTrack) HandleMasterEnd(id gomkv.ElementID, info gomkv.ElementInfo) error {
	if id == gomkv.TrackEntryElement && h.inEntry {
		if h.entryCodec == "A_OPUS" && h.entryNumber != 0 {
			h.number, h.codec, h.channels, h.codecPrivate = h.entryNumber, h.entryCodec, h.entryChans, h.entryPrivate
			if h.channels == 0 {
				h.channels = 2
			}
		}
		h.inEntry = false
	}
	return nil
}

func (h *webmOpusTrack) HandleBinary(id gomkv.ElementID, value []byte, info gomkv.ElementInfo) error {
	if h.inEntry && id == gomkv.CodecPrivateElement {
		h.entryPrivate = append(h.entryPrivate[:0], value...)
	}
	return nil
}

func (h *webmOpusTrack) HandleInteger(id gomkv.ElementID, value int64, info gomkv.ElementInfo) error {
	if !h.inEntry || value < 0 {
		return nil
	}
	switch id {
	case gomkv.TrackNumberElement:
		h.entryNumber = uint64(value)
	case gomkv.ChannelsElement:
		h.entryChans = int(value)
	}
	return nil
}

func (h *webmOpusTrack) HandleString(id gomkv.ElementID, value string, info gomkv.ElementInfo) error {
	if h.inEntry && id == gomkv.CodecIDElement {
		h.entryCodec = value
	}
	return nil
}

type webmOpusBlocks struct {
	gomkv.DefaultHandler
	track   uint64
	writer  *ogg.Writer
	packets int
}

func (h *webmOpusBlocks) HandleBinary(id gomkv.ElementID, block []byte, info gomkv.ElementInfo) error {
	if id != gomkv.SimpleBlockElement && id != gomkv.BlockElement {
		return nil
	}
	track, header, err := readEBMLVInt(block)
	if err != nil {
		return err
	}
	if track != h.track {
		return nil
	}
	if len(block) < header+3 {
		return errors.New("truncated WebM audio block")
	}
	flags := block[header+2]
	packets, err := splitWebMBlock(block[header+3:], flags)
	if err != nil {
		return err
	}
	for _, packet := range packets {
		samples, err := opusPacketSamples(packet)
		if err != nil {
			return err
		}
		if err := h.writer.WritePacket(packet, samples); err != nil {
			return err
		}
		h.packets++
	}
	return nil
}

func readEBMLVInt(data []byte) (uint64, int, error) {
	if len(data) == 0 || data[0] == 0 {
		return 0, 0, errors.New("invalid WebM block track number")
	}
	mask := byte(0x80)
	length := 1
	for data[0]&mask == 0 {
		mask >>= 1
		length++
	}
	if len(data) < length {
		return 0, 0, errors.New("truncated WebM block track number")
	}
	value := uint64(data[0] & (mask - 1))
	for _, b := range data[1:length] {
		value = value<<8 | uint64(b)
	}
	return value, length, nil
}

func splitWebMBlock(data []byte, flags byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty WebM audio block")
	}
	lacing := (flags >> 1) & 0x03
	if lacing == 0 {
		return [][]byte{data}, nil
	}
	if len(data) < 1 {
		return nil, errors.New("truncated WebM lacing header")
	}
	count := int(data[0]) + 1
	data = data[1:]
	sizes := make([]int, count)
	switch lacing {
	case 1: // Xiph lacing
		for i := 0; i < count-1; i++ {
			for {
				if len(data) == 0 {
					return nil, errors.New("truncated Xiph lacing size")
				}
				b := int(data[0])
				data = data[1:]
				sizes[i] += b
				if b != 255 {
					break
				}
			}
		}
	case 2: // Fixed lacing
		if len(data)%count != 0 {
			return nil, errors.New("invalid fixed WebM lacing size")
		}
		for i := range sizes {
			sizes[i] = len(data) / count
		}
	case 3: // EBML lacing
		first, n, err := readEBMLVInt(data)
		if err != nil {
			return nil, err
		}
		sizes[0] = int(first)
		data = data[n:]
		for i := 1; i < count-1; i++ {
			delta, n, err := readSignedEBMLLace(data)
			if err != nil {
				return nil, err
			}
			sizes[i] = sizes[i-1] + delta
			data = data[n:]
			if sizes[i] < 0 {
				return nil, errors.New("negative EBML lace size")
			}
		}
	default:
		return nil, errors.New("unsupported WebM lacing mode")
	}
	used := 0
	for i := 0; i < count-1; i++ {
		if sizes[i] < 0 || sizes[i] > len(data)-used {
			return nil, errors.New("invalid WebM lace size")
		}
		used += sizes[i]
	}
	sizes[count-1] = len(data) - used
	packets := make([][]byte, 0, count)
	for _, size := range sizes {
		if size <= 0 || size > len(data) {
			return nil, errors.New("invalid empty WebM audio packet")
		}
		packets = append(packets, data[:size])
		data = data[size:]
	}
	return packets, nil
}

func readSignedEBMLLace(data []byte) (int, int, error) {
	value, n, err := readEBMLVInt(data)
	if err != nil {
		return 0, 0, err
	}
	bits := uint(n * 7)
	bias := (uint64(1) << (bits - 1)) - 1
	return int(value) - int(bias), n, nil
}

func opusPacketSamples(packet []byte) (int, error) {
	if len(packet) == 0 {
		return 0, errors.New("empty Opus packet in WebM")
	}
	toc := packet[0]
	config := toc >> 3
	var frameSamples int
	switch {
	case config < 12:
		frameSamples = [...]int{480, 960, 1920, 2880}[config&3]
	case config < 16:
		frameSamples = [...]int{480, 960}[config&1]
	default:
		frameSamples = [...]int{120, 240, 480, 960}[config&3]
	}
	count := 1
	switch toc & 3 {
	case 1, 2:
		count = 2
	case 3:
		if len(packet) < 2 {
			return 0, errors.New("truncated Opus packet frame count")
		}
		count = int(packet[1] & 0x3f)
		if count == 0 {
			return 0, errors.New("invalid Opus packet frame count")
		}
	}
	if frameSamples*count > 5760 {
		return 0, errors.New("Opus packet exceeds 120 ms")
	}
	return frameSamples * count, nil
}
