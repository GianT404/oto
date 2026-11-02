package player

import (
	"encoding/binary"
	"io"
	"math"
	"sync/atomic"
)

type nativePreset int

const (
	presetNormal nativePreset = iota
	presetBass
	presetNightcore
	presetDaycore
	presetLoFi
)

func nativePresetFor(filter string) (nativePreset, float64, bool) {
	switch {
	case filter == "dynaudnorm=f=250:g=11:p=0.9:m=10":
		return presetNormal, 1, true
	case filter == "dynaudnorm=f=250:g=11:p=0.9:m=10,bass=g=8:f=100:w=0.5":
		return presetBass, 1, true
	case filter == "dynaudnorm=f=250:g=11:p=0.9:m=10,asetrate=48000*1.2,aresample=44100":
		return presetNightcore, 1.2, true
	case filter == "dynaudnorm=f=250:g=11:p=0.9:m=10,asetrate=48000*0.85,aresample=48000,aecho=0.8:0.88:60:0.4":
		return presetDaycore, 0.85, true
	case filter == "dynaudnorm=f=250:g=11:p=0.9:m=10,lowpass=f=800,volume=1.2":
		return presetLoFi, 1, true
	default:
		return presetNormal, 1, false
	}
}

type presetPCMReader struct {
	source io.ReadCloser
	mode   nativePreset
	low    [2]float64
	echo   [][2]float64
	index  int
	eof    atomic.Bool
}

func applyNativePreset(source io.ReadCloser, mode nativePreset) io.ReadCloser {
	if mode == presetNormal {
		return source
	}
	r := &presetPCMReader{source: source, mode: mode}
	if mode == presetDaycore {
		r.echo = make([][2]float64, 2880) // 60 ms at 48 kHz
	}
	return r
}

func (r *presetPCMReader) Read(dst []byte) (int, error) {
	n, err := r.source.Read(dst)
	if err != nil {
		r.eof.Store(true)
	}
	frames := n / 4
	for i := 0; i < frames; i++ {
		left := float64(int16(binary.LittleEndian.Uint16(dst[i*4:])))
		right := float64(int16(binary.LittleEndian.Uint16(dst[i*4+2:])))
		switch r.mode {
		case presetBass:
			left = r.bass(left, 0)
			right = r.bass(right, 1)
		case presetLoFi:
			left = r.lowpass(left, 0, 800) * 1.2
			right = r.lowpass(right, 1, 800) * 1.2
		case presetDaycore:
			delayed := r.echo[r.index]
			r.echo[r.index] = [2]float64{left + delayed[0]*0.4, right + delayed[1]*0.4}
			r.index = (r.index + 1) % len(r.echo)
			left = left*0.8 + delayed[0]*0.88
			right = right*0.8 + delayed[1]*0.88
		}
		binary.LittleEndian.PutUint16(dst[i*4:], uint16(clampInt16(left)))
		binary.LittleEndian.PutUint16(dst[i*4+2:], uint16(clampInt16(right)))
	}
	return n, err
}

func (r *presetPCMReader) lowpass(sample float64, channel int, cutoff float64) float64 {
	alpha := 1 - math.Exp(-2*math.Pi*cutoff/48000)
	r.low[channel] += alpha * (sample - r.low[channel])
	return r.low[channel]
}

func (r *presetPCMReader) bass(sample float64, channel int) float64 {
	low := r.lowpass(sample, channel, 100)
	return sample + low*(math.Pow(10, 8.0/20.0)-1)
}

func (r *presetPCMReader) Close() error   { return r.source.Close() }
func (r *presetPCMReader) finished() bool { return r.eof.Load() }

func clampInt16(sample float64) int16 {
	if sample > 32767 {
		sample = 32767
	} else if sample < -32768 {
		sample = -32768
	}
	return int16(sample)
}
