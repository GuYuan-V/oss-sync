package auth

import (
	"sync"
	"time"
)

// AttemptLimiter 限制进程内同一凭据键在时间窗口中的尝试次数
type AttemptLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string][]time.Time
}

// NewAttemptLimiter 创建凭据端点的限流器
func NewAttemptLimiter(limit int, window time.Duration) *AttemptLimiter {
	return &AttemptLimiter{limit: limit, window: window, entries: map[string][]time.Time{}}
}

func (l *AttemptLimiter) Allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := l.entries[key]
	kept := entries[:0]
	for _, at := range entries {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= l.limit {
		l.entries[key] = kept
		return false
	}
	l.entries[key] = append(kept, now)
	return true
}
