// 凭据端点的进程内限流。
package auth

import (
	"sync"
	"time"
)

// AttemptLimiter 为凭据端点的进程内小守卫。服务为单实例部署，该守卫足以拦截针对高成本 bcrypt 计算的重复调用。
type AttemptLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string][]time.Time
}

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
