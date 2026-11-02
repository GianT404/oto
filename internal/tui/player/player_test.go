package player

import (
	"sync"
	"testing"
	"time"
)

func newTestPlayer() *Player {
	return &Player{state: Stopped}
}

func TestPlayer_ConcurrentPlayStopSeek_NoRaceNoDeadlock(t *testing.T) {
	p := newTestPlayer()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				_ = p.Play("fake.opus")
			case 1:
				p.Stop()
			case 2:
				p.SeekBy(5)
			}
		}(i)
	}
	wg.Wait()

	switch p.State() {
	case Stopped, Playing, Paused:
	default:
		t.Fatalf("state không hợp lệ : %v", p.State())
	}
}

func TestPlayer_Stop_IdempotentWhenAlreadyStopped(t *testing.T) {
	p := newTestPlayer()

	p.Stop()
	p.Stop()
	p.Stop()

	if p.State() != Stopped {
		t.Fatalf("state không hợp lệ : %v", p.State())
	}
}

func TestPlayer_PlayFrom_CallsStopLockedBeforeOtoCtxCheck(t *testing.T) {
	p := newTestPlayer()
	p.state = Playing

	err := p.Play("fake.opus")
	if err == nil {
		t.Fatal("otoCtx nil thi playFrom phai tra ve loi")
	}
	if p.State() != Stopped {
		t.Fatalf("state không hợp lệ : %v", p.State())
	}
}

func TestPlayer_StopInvalidatesGeneration(t *testing.T) {
	p := newTestPlayer()
	before := p.Generation()

	p.Stop()
	if got := p.Generation(); got <= before {
		t.Fatalf("generation=%d, want > %d", got, before)
	}

	p.Stop()
	if got := p.Generation(); got <= before+1 {
		t.Fatalf("generation=%d, want > %d", got, before+1)
	}
}

func TestPlayer_PlayWithHeadersIfGenerationRejectsStaleRequest(t *testing.T) {
	p := newTestPlayer()
	p.generation = 4

	started, err := p.PlayWithHeadersIfGeneration("fake.opus", nil, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if started {
		t.Fatal("stale generation should not start playback")
	}
	if p.Generation() != 4 {
		t.Fatalf("generation=%d, want 4", p.Generation())
	}
}

func TestPlayer_PositionUsesPendingSeekTarget(t *testing.T) {
	p := newTestPlayer()
	p.state = Playing
	p.startTime = time.Now().Add(-30 * time.Second)
	p.seekPending = true
	p.pendingPosition = 75 * time.Second

	if got := p.Position(); got != 75*time.Second {
		t.Fatalf("Position() = %s, want pending seek target 1m15s", got)
	}
}

func TestPlayer_StopClearsPendingSeek(t *testing.T) {
	p := newTestPlayer()
	p.state = Playing
	p.currentPath = "track.opus"
	p.seekPending = true
	p.pendingPosition = 42 * time.Second
	before := p.seekGeneration

	p.Stop()

	if p.seekPending {
		t.Fatal("Stop should clear pending seek state")
	}
	if p.pendingPosition != 0 {
		t.Fatalf("pending position = %s, want 0", p.pendingPosition)
	}
	if p.seekGeneration <= before {
		t.Fatalf("seek generation = %d, want greater than %d", p.seekGeneration, before)
	}
}
