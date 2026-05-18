package events

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// In-process pub/sub for live SSE fan-out. Persistence is handled by store.AppendEvent;
// the bus only carries the "something new for this deployment" signal so SSE can
// re-query the DB and stream. Cross-process delivery is via DB polling (fine for M1).

type Bus struct {
	mu   sync.Mutex
	subs map[uuid.UUID]map[chan struct{}]struct{}
}

func NewBus() *Bus {
	return &Bus{subs: map[uuid.UUID]map[chan struct{}]struct{}{}}
}

func (b *Bus) Subscribe(deploymentID uuid.UUID) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 16)
	b.mu.Lock()
	if b.subs[deploymentID] == nil {
		b.subs[deploymentID] = map[chan struct{}]struct{}{}
	}
	b.subs[deploymentID][ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if m, ok := b.subs[deploymentID]; ok {
			delete(m, ch)
			if len(m) == 0 {
				delete(b.subs, deploymentID)
			}
		}
		close(ch)
	}
	return ch, cancel
}

func (b *Bus) Publish(deploymentID uuid.UUID) {
	b.mu.Lock()
	m := b.subs[deploymentID]
	chs := make([]chan struct{}, 0, len(m))
	for ch := range m {
		chs = append(chs, ch)
	}
	b.mu.Unlock()
	for _, ch := range chs {
		select {
		case ch <- struct{}{}:
		default:
			// drop — receiver will re-poll on next signal
		}
	}
}

// PublishCtx is Publish that bails on ctx done.
func (b *Bus) PublishCtx(ctx context.Context, deploymentID uuid.UUID) {
	if ctx.Err() != nil {
		return
	}
	b.Publish(deploymentID)
}
