package rate

import (
	"sync"
	"time"
)

type Limiter interface {
	Allow(key string, limit int, window time.Duration) (bool, time.Duration)
}

type MemoryLimiter struct {
	mu    sync.Mutex
	store map[string]*bucket
}

type bucket struct {
	count   int
	resetAt time.Time
	window  time.Duration
}

func NewMemory() *MemoryLimiter {
	return &MemoryLimiter{store: make(map[string]*bucket)}
}

func (m *MemoryLimiter) Allow(key string, limit int, window time.Duration) (bool, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	b, ok := m.store[key]
	if !ok || now.After(b.resetAt) || b.window != window {
		b = &bucket{count: 0, resetAt: now.Add(window), window: window}
		m.store[key] = b
	}

	if b.count >= limit {
		return false, time.Until(b.resetAt)
	}

	b.count++
	return true, time.Until(b.resetAt)
}
