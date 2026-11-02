package player

import (
	"encoding/binary"
	"io"
	"math"
	"sync/atomic"
)

const (
	wsolaWindowFrames  = 960 // 20 ms at the player's 48 kHz output rate.
	wsolaOverlapFrames = 480 // 50% overlap; one 10 ms synthesis hop.
	wsolaSearchFrames  = 240 // Search +/- 5 ms for a similar waveform segment.
	wsolaSearchStep    = 4
)

type stereoPCMFrame [2]int16

// speedPCMReader changes the duration of interleaved stereo S16 PCM while
// preserving pitch. It uses WSOLA to align overlapping windows at similar
// waveform points before crossfading them.
type speedPCMReader struct {
	source io.ReadCloser
	speed  float64

	input     []stereoPCMFrame
	inputBase int64
	inputEOF  bool

	initialized bool
	previousAt  int64
	expectedAt  int64
	previous    []stereoPCMFrame

	output []stereoPCMFrame
	outAt  int
	done   bool
	eof    atomic.Bool
	err    atomic.Pointer[error]
}

func withPlaybackSpeed(source io.ReadCloser, speed float64) io.ReadCloser {
	if speed <= 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
		speed = 1
	}
	if speed == 1 {
		return source
	}
	return &speedPCMReader{
		source:   source,
		speed:    speed,
		previous: make([]stereoPCMFrame, wsolaOverlapFrames),
	}
}

func (r *speedPCMReader) Read(dst []byte) (int, error) {
	capacity := len(dst) / 4
	if capacity == 0 {
		return 0, io.ErrShortBuffer
	}
	if r.outAt > 0 {
		copy(r.output, r.output[r.outAt:])
		r.output = r.output[:len(r.output)-r.outAt]
		r.outAt = 0
	}
	for len(r.output)-r.outAt < capacity && !r.done {
		r.generate()
	}

	available := len(r.output) - r.outAt
	frames := min(capacity, available)
	for i := 0; i < frames; i++ {
		frame := r.output[r.outAt+i]
		binary.LittleEndian.PutUint16(dst[i*4:], uint16(frame[0]))
		binary.LittleEndian.PutUint16(dst[i*4+2:], uint16(frame[1]))
	}
	r.outAt += frames
	if r.outAt == len(r.output) {
		r.output = r.output[:0]
		r.outAt = 0
	}
	if frames == 0 {
		r.eof.Store(true)
		if err := r.err.Load(); err != nil {
			return 0, *err
		}
		return 0, io.EOF
	}
	return frames * 4, nil
}

func (r *speedPCMReader) generate() {
	if !r.initialized {
		r.ensure(wsolaWindowFrames)
		if len(r.input) == 0 {
			r.done = true
			return
		}
		if len(r.input) < wsolaWindowFrames {
			r.output = append(r.output, r.input...)
			r.done = true
			return
		}
		copy(r.previous, r.input[wsolaOverlapFrames:wsolaWindowFrames])
		r.output = append(r.output, r.input[:wsolaOverlapFrames]...)
		r.previousAt = r.inputBase
		r.expectedAt = r.previousAt + r.inputHop()
		r.initialized = true
		return
	}

	searchStart := max(r.expectedAt-wsolaSearchFrames, r.previousAt+max(1, r.inputHop()/2))
	searchStart = max(searchStart, r.inputBase)
	searchEnd := r.expectedAt + wsolaSearchFrames
	r.ensure(searchEnd + wsolaWindowFrames)
	inputEnd := r.inputBase + int64(len(r.input))
	fullWindowEnd := inputEnd - wsolaWindowFrames

	comparisonFrames := wsolaOverlapFrames
	partial := false
	if fullWindowEnd < searchStart {
		partial = true
		available := inputEnd - searchStart
		if available <= 0 {
			r.done = true
			return
		}
		comparisonFrames = min(wsolaOverlapFrames, int(available))
		fullWindowEnd = inputEnd - int64(comparisonFrames)
	}
	searchEnd = min(searchEnd, fullWindowEnd)
	if searchEnd < searchStart {
		r.done = true
		return
	}

	candidateAt := r.bestCandidate(searchStart, searchEnd, comparisonFrames)
	offset := int(candidateAt - r.inputBase)
	for i := 0; i < comparisonFrames; i++ {
		fadeIn := float64(i) / float64(max(1, comparisonFrames-1))
		fadeOut := 1 - fadeIn
		left := float64(r.previous[i][0])*fadeOut + float64(r.input[offset+i][0])*fadeIn
		right := float64(r.previous[i][1])*fadeOut + float64(r.input[offset+i][1])*fadeIn
		r.output = append(r.output, stereoPCMFrame{int16(left), int16(right)})
	}

	if partial {
		end := int(inputEnd - r.inputBase)
		r.output = append(r.output, r.input[offset+comparisonFrames:end]...)
		r.done = true
		return
	}

	copy(r.previous, r.input[offset+wsolaOverlapFrames:offset+wsolaWindowFrames])
	r.previousAt = candidateAt
	r.expectedAt = candidateAt + r.inputHop()
	r.discardBefore(max(r.inputBase, r.expectedAt-wsolaSearchFrames-wsolaSearchStep))
}

func (r *speedPCMReader) inputHop() int64 {
	hop := int64(math.Round(float64(wsolaOverlapFrames) * r.speed))
	return max(1, hop)
}

func (r *speedPCMReader) ensure(end int64) {
	for r.inputBase+int64(len(r.input)) < end && !r.inputEOF {
		missing := end - (r.inputBase + int64(len(r.input)))
		readFrames := min(missing, int64(2048))
		buffer := make([]byte, int(readFrames)*4)
		n, err := io.ReadFull(r.source, buffer)
		completeFrames := n / 4
		for i := 0; i < completeFrames; i++ {
			r.input = append(r.input, stereoPCMFrame{
				int16(binary.LittleEndian.Uint16(buffer[i*4:])),
				int16(binary.LittleEndian.Uint16(buffer[i*4+2:])),
			})
		}
		if n%4 != 0 {
			storePlaybackError(&r.err, io.ErrUnexpectedEOF)
			r.inputEOF = true
		}
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				storePlaybackError(&r.err, err)
			}
			r.inputEOF = true
		}
	}
}

func (r *speedPCMReader) bestCandidate(start, end int64, frames int) int64 {
	bestAt := start
	bestScore := math.Inf(-1)
	for candidate := start; candidate <= end; candidate += wsolaSearchStep {
		offset := int(candidate - r.inputBase)
		score := waveformCorrelation(r.previous[:frames], r.input[offset:offset+frames])
		if score > bestScore {
			bestScore = score
			bestAt = candidate
		}
	}
	return bestAt
}

func waveformCorrelation(reference, candidate []stereoPCMFrame) float64 {
	var dot, refEnergy, candidateEnergy float64
	for i := 0; i < len(reference); i += 4 {
		for channel := range reference[i] {
			a := float64(reference[i][channel])
			b := float64(candidate[i][channel])
			dot += a * b
			refEnergy += a * a
			candidateEnergy += b * b
		}
	}
	denominator := math.Sqrt(refEnergy * candidateEnergy)
	if denominator == 0 {
		return 0
	}
	return dot / denominator
}

func (r *speedPCMReader) discardBefore(frame int64) {
	count := int(min(frame-r.inputBase, int64(len(r.input))))
	if count <= 0 {
		return
	}
	copy(r.input, r.input[count:])
	r.input = r.input[:len(r.input)-count]
	r.inputBase += int64(count)
}

func (r *speedPCMReader) Close() error   { return r.source.Close() }
func (r *speedPCMReader) finished() bool { return r.eof.Load() }

func (r *speedPCMReader) playbackError() error {
	if err := r.err.Load(); err != nil {
		return *err
	}
	return playbackErrorFrom(r.source)
}
