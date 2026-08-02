package events

import "sync"

type Bus[T any] struct {
	mu          sync.RWMutex
	subscribers map[chan T]struct{}
	closed      bool
}

func NewBus[T any]() *Bus[T] {
	return &Bus[T]{
		subscribers: make(map[chan T]struct{}),
	}
}

func (b *Bus[T]) Subscribe(buffer int) <-chan T {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan T, buffer)
	if b.closed {
		close(ch)
		return ch
	}
	b.subscribers[ch] = struct{}{}
	return ch
}

func (b *Bus[T]) Unsubscribe(ch <-chan T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for candidate := range b.subscribers {
		if (<-chan T)(candidate) == ch {
			delete(b.subscribers, candidate)
			close(candidate)
			return
		}
	}
}

func (b *Bus[T]) Publish(value T) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}
	for ch := range b.subscribers {
		select {
		case ch <- value:
		default:
		}
	}
}

func (b *Bus[T]) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true
	for ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, ch)
	}
}
