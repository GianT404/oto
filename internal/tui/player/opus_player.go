package player

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/oto/v3"
)

type State int

const (
	Stopped State = iota
	Playing
	Paused
)

type gaplessReader struct {
	buf    []byte
	offset int
	pipe   io.ReadCloser
	eof    atomic.Bool
	err    atomic.Pointer[error]
}

func (r *gaplessReader) Read(p []byte) (n int, err error) {
	if r.offset < len(r.buf) {
		n = copy(p, r.buf[r.offset:])
		r.offset += n
		return n, nil
	}
	n, err = r.pipe.Read(p)
	if err != nil {
		r.eof.Store(true)
		if err != io.EOF {
			storePlaybackError(&r.err, err)
		}
	}
	return n, err
}

func (r *gaplessReader) Close() error {
	if r.pipe != nil {
		return r.pipe.Close()
	}
	return nil
}

func (r *gaplessReader) playbackError() error {
	if err := r.err.Load(); err != nil {
		return *err
	}
	return playbackErrorFrom(r.pipe)
}

type Player struct {
	mu           sync.Mutex
	state        State
	otoCtx       *oto.Context
	player       *oto.Player
	directReader io.ReadCloser
	currentPath  string
	headers      map[string]string
	startTime    time.Time
	pauseOffset  time.Duration
	lastErr      error
	stopped      bool
	volume       float64
	generation      uint64
	seekGeneration  uint64
	seekPending     bool
	pendingPosition time.Duration
	seekErr         error
	audioFilter     string
	speed        float64
	skipSilence  bool

	// Gapless playback: pre-decoded next track.
	nextPipe  io.ReadCloser
	nextBuf   []byte
	nextPath  string
	nextGen   uint64
	nextReady bool
}

func New() *Player {
	op := &oto.NewContextOptions{
		SampleRate:   48000,
		ChannelCount: 2,
		Format:       oto.FormatSignedInt16LE,
	}
	ctx, ready, err := oto.NewContext(op)
	if err != nil {
		fmt.Printf("oto init error: %v\n", err)
		return &Player{state: Stopped}
	}

	<-ready

	return &Player{
		otoCtx:      ctx,
		state:       Stopped,
		volume:      1.0,
		speed:       1.0,
		audioFilter: "dynaudnorm=f=250:g=11:p=0.9:m=10",
	}
}

func (p *Player) SetSpeed(s float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s <= 0 {
		s = 1.0
	}
	p.speed = s
}

func (p *Player) SetAudioFilter(filter string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if filter == "" {
		filter = "dynaudnorm=f=250:g=11:p=0.9:m=10"
	}
	p.audioFilter = filter
}

func (p *Player) SetSkipSilence(on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.skipSilence = on
}
func (p *Player) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// positionLocked returns the current playback position. The caller must hold p.mu.
func (p *Player) positionLocked() time.Duration {
	if p.seekPending {
		return p.pendingPosition
	}
	speed := p.speed
	if speed <= 0 {
		speed = 1.0
	}
	switch p.state {
	case Playing:
		return time.Duration(float64(time.Since(p.startTime)) * speed)
	case Paused:
		return p.pauseOffset
	default:
		return 0
	}
}

// Position trả về vị trí phát hiện tại (tính cả trạng thái pause và seek đang chờ).
func (p *Player) Position() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.positionLocked()
}

func (p *Player) Play(filePath string) error {
	return p.playFrom(filePath, 0, nil)
}

// PlayWithHeaders plays a URL or file with optional HTTP request headers.

func (p *Player) PlayWithHeaders(filePath string, headers map[string]string) error {
	return p.playFrom(filePath, 0, headers)
}

// PlayWithHeadersIfGeneration chỉ phát nếu generation vẫn còn hiệu lực.
func (p *Player) PlayWithHeadersIfGeneration(filePath string, headers map[string]string, expected uint64) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.generation != expected {
		return false, nil
	}
	return true, p.playFromLocked(filePath, 0, headers)
}

func (p *Player) playFrom(filePath string, startSec float64, headers map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playFromLocked(filePath, startSec, headers)
}

func (p *Player) playFromLocked(filePath string, startSec float64, headers map[string]string) error {
	p.stopLocked()

	if p.otoCtx == nil {
		return fmt.Errorf("oto context is not initialized")
	}

	speed := p.speed
	if speed <= 0 {
		speed = 1.0
	}

	p.currentPath = filePath
	p.headers = headers
	p.startTime = time.Now().Add(-time.Duration((startSec / speed) * float64(time.Second)))
	p.pauseOffset = 0
	p.lastErr = nil
	p.seekErr = nil
	p.seekPending = false
	p.pendingPosition = 0
	p.stopped = false
	p.generation++
	reader, err := openPlaybackReader(filePath, headers, startSec, speed, p.audioFilter, p.skipSilence)
	if err != nil {
		p.lastErr = err
		p.currentPath = ""
		p.headers = nil
		return fmt.Errorf("open audio: %w", err)
	}
	p.seekPending = false
	p.seekErr = nil
	p.directReader = reader
	p.player = p.otoCtx.NewPlayer(reader)
	p.player.SetVolume(p.volume)
	p.player.Play()
	p.state = Playing
	go p.watchDirectPlayback(reader, p.generation, p.player)
	return nil
}

func (p *Player) watchDirectPlayback(reader io.ReadCloser, gen uint64, optr *oto.Player) {
	finished := func() bool {
		switch direct := reader.(type) {
		case *oggOpusReader:
			return direct.eof.Load()
		case *beepAudioReader:
			return direct.finished()
		case *speedPCMReader:
			return direct.finished()
		case *silencePCMReader:
			return direct.finished()
		case *gaplessReader:
			return direct.eof.Load()
		case *presetPCMReader:
			return direct.finished()
		default:
			return false
		}
	}
	for optr != nil {
		time.Sleep(50 * time.Millisecond)
		p.mu.Lock()
		active := p.directReader == reader && p.generation == gen && (p.state == Playing || p.state == Paused)
		p.mu.Unlock()
		if !active || (finished() && !optr.IsPlaying()) {
			break
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.directReader == reader && p.generation == gen {
		_ = reader.Close()
		p.directReader = nil
		if readErr := playbackErrorFrom(reader); readErr != nil {
			p.lastErr = readErr
		}
		p.state = Stopped
		p.currentPath = ""
	}
}

func (p *Player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Tăng generation để hủy mọi resolve/predecode cũ.
	p.generation++
	p.stopLocked()
}
func (p *Player) stopLocked() {
	p.seekGeneration++
	p.seekPending = false
	p.pendingPosition = 0
	p.seekErr = nil
	if p.state != Stopped {
		p.stopped = true
		if p.player != nil {
			_ = p.player.Close()
		}
		if p.directReader != nil {
			_ = p.directReader.Close()
			p.directReader = nil
		}
		p.state = Stopped
		p.currentPath = ""
		p.headers = nil
	}
	p.cancelPreDecodeLocked()
}

// PlaybackError returns a decoder error from the most recent playback, if any.
func (p *Player) PlaybackError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}
func (p *Player) TogglePause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.player == nil || p.state == Stopped {
		return
	}
	speed := p.speed
	if speed <= 0 {
		speed = 1.0
	}
	if p.state == Playing {
		position := p.positionLocked()
		p.player.Pause()
		p.state = Paused
		p.pauseOffset = position
	} else if p.state == Paused {
		p.player.Play()
		p.state = Playing
		p.startTime = time.Now().Add(-time.Duration(float64(p.pauseOffset) / speed))
	}
}

// SeekBy queues an asynchronous seek relative to the logical playback position.
// Repeated key events supersede older requests instead of blocking the UI loop.
func (p *Player) SeekBy(seconds float64) {
	p.mu.Lock()
	if p.currentPath == "" || p.state == Stopped {
		p.mu.Unlock()
		return
	}
	target := p.positionLocked().Seconds() + seconds
	p.mu.Unlock()
	p.SeekTo(target)
}

// SeekTo prepares a replacement reader in the background. The current audio
// keeps playing until the new decoder is ready, then the output player is swapped.
func (p *Player) SeekTo(seconds float64) {
	p.mu.Lock()
	if p.currentPath == "" || p.state == Stopped || p.otoCtx == nil {
		p.mu.Unlock()
		return
	}
	if seconds < 0 {
		seconds = 0
	}
	if p.speed <= 0 {
		p.speed = 1
	}

	p.seekGeneration++
	requestID := p.seekGeneration
	trackGeneration := p.generation
	path := p.currentPath
	headers := cloneHeaders(p.headers)
	speed := p.speed
	filter := p.audioFilter
	skip := p.skipSilence
	target := time.Duration(seconds * float64(time.Second))
	p.seekPending = true
	p.pendingPosition = target
	p.seekErr = nil
	p.mu.Unlock()

	go p.completeSeek(requestID, trackGeneration, path, headers, seconds, target, speed, filter, skip)
}

func (p *Player) completeSeek(
	requestID, trackGeneration uint64,
	path string,
	headers map[string]string,
	seconds float64,
	target time.Duration,
	speed float64,
	filter string,
	skip bool,
) {
	reader, err := openPlaybackReader(path, headers, seconds, speed, filter, skip)

	p.mu.Lock()
	if requestID != p.seekGeneration || trackGeneration != p.generation || p.state == Stopped {
		p.mu.Unlock()
		if reader != nil {
			_ = reader.Close()
		}
		return
	}
	if err != nil {
		p.seekPending = false
		p.pendingPosition = 0
		p.seekErr = err
		p.mu.Unlock()
		return
	}

	oldPlayer := p.player
	oldReader := p.directReader
	newPlayer := p.otoCtx.NewPlayer(reader)
	newPlayer.SetVolume(p.volume)

	if oldPlayer != nil {
		_ = oldPlayer.Close()
	}
	if oldReader != nil {
		_ = oldReader.Close()
	}

	p.player = newPlayer
	p.directReader = reader
	p.pauseOffset = target
	p.seekPending = false
	p.pendingPosition = 0
	p.seekErr = nil
	p.lastErr = nil
	gen := p.generation
	if p.state == Playing {
		p.startTime = time.Now().Add(-time.Duration(float64(target) / speed))
		newPlayer.Play()
	}
	p.mu.Unlock()

	go p.watchDirectPlayback(reader, gen, newPlayer)
}

// openPlaybackReader keeps the decoder and DSP pipeline identical for normal
// playback and seeking. Native seekable formats use their decoder's sample seek.
func openPlaybackReader(
	path string,
	headers map[string]string,
	startSec float64,
	speed float64,
	filter string,
	skipSilenceEnabled bool,
) (io.ReadCloser, error) {
	mode, presetSpeed, supported := nativePresetFor(filter)
	if !supported {
		return nil, fmt.Errorf("unsupported audio filter")
	}

	var reader io.ReadCloser
	var err error
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		var ok bool
		reader, ok, err = openNativeURL(path, headers, startSec)
		if err == nil && !ok {
			err = fmt.Errorf("unsupported audio URL format")
		}
	} else {
		reader, err = openNativeFile(path, startSec)
	}
	if err != nil {
		return nil, err
	}

	reader = applyNativePreset(reader, mode)
	if skipSilenceEnabled {
		reader = skipSilence(reader)
	}
	reader = withPlaybackSpeed(reader, speed*presetSpeed)
	return reader, nil
}

func cloneHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	copyHeaders := make(map[string]string, len(headers))
	for key, value := range headers {
		copyHeaders[key] = value
	}
	return copyHeaders
}

// currentHeaders giữ headers của lần phát hiện hiện tại.
func (p *Player) currentHeaders() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return cloneHeaders(p.headers)
}

func (p *Player) Stderr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.seekErr != nil {
		return "seek: " + p.seekErr.Error()
	}
	if p.lastErr != nil {
		return p.lastErr.Error()
	}
	return ""
}

func (p *Player) SetVolume(v float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if v < 0.0 {
		v = 0.0
	}
	if v > 2.0 {
		v = 2.0
	}

	p.volume = v
	if p.player != nil {
		p.player.SetVolume(v)
	}
}

func (p *Player) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

func (p *Player) Generation() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.generation
}

func (p *Player) CurrentPath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentPath
}

func (p *Player) PreDecodeNext(filePath string, headers map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.cancelPreDecodeLocked()

	speed := p.speed
	if speed <= 0 {
		speed = 1.0
	}
	mode, presetSpeed, native := nativePresetFor(p.audioFilter)
	if !native {
		return
	}
	var pipe io.ReadCloser
	var err error
	if strings.HasPrefix(filePath, "http://") || strings.HasPrefix(filePath, "https://") {
		var ok bool
		pipe, ok, err = openNativeURL(filePath, headers, 0)
		if err == nil && !ok {
			return
		}
	} else {
		pipe, err = openNativeFile(filePath, 0)
	}
	if err != nil || pipe == nil {
		return
	}
	pipe = applyNativePreset(pipe, mode)
	if p.skipSilence {
		pipe = skipSilence(pipe)
	}
	pipe = withPlaybackSpeed(pipe, speed*presetSpeed)

	p.nextPipe = pipe
	p.nextPath = filePath
	p.nextGen = p.generation
	p.nextReady = false

	const maxBuf = 960000
	go func() {
		buf := make([]byte, 0, maxBuf)
		tmp := make([]byte, 4096)
		for len(buf) < maxBuf {
			n, err := pipe.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				break
			}
		}

		p.mu.Lock()
		defer p.mu.Unlock()

		if p.nextPipe != pipe || p.generation != p.nextGen {
			_ = pipe.Close()
			return
		}

		p.nextBuf = buf
		p.nextReady = true
	}()
}

func (p *Player) CancelPreDecode() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelPreDecodeLocked()
}

func (p *Player) cancelPreDecodeLocked() {
	if p.nextPipe != nil {
		_ = p.nextPipe.Close()
		p.nextPipe = nil
		p.nextBuf = nil
		p.nextReady = false
	}
}

// PlayFromBuffer starts playing the pre-decoded next track seamlessly.
// Returns true if gapless playback was used, false if fallback to normal Play().
func (p *Player) PlayFromBuffer() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.nextReady || p.nextPipe == nil {
		return false
	}

	// Save pre-decoded data before stopLocked() clears them via cancelPreDecodeLocked().
	pipe := p.nextPipe
	buf := p.nextBuf
	nextPath := p.nextPath
	p.nextPipe = nil
	p.nextBuf = nil
	p.nextReady = false

	p.stopLocked()

	if p.otoCtx == nil {
		_ = pipe.Close()
		return false
	}

	p.currentPath = nextPath
	p.headers = nil
	p.startTime = time.Now()
	p.pauseOffset = 0
	p.lastErr = nil
	p.stopped = false
	p.generation++

	reader := &gaplessReader{buf: buf, pipe: pipe}
	p.directReader = reader

	p.player = p.otoCtx.NewPlayer(reader)
	p.player.SetVolume(p.volume)
	p.player.Play()
	p.state = Playing

	go p.watchDirectPlayback(reader, p.generation, p.player)

	return true
}
