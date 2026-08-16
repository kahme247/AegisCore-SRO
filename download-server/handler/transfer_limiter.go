package handler

import (
	"errors"
	"sync"
)

var ErrTransferLimit = errors.New("download transfer concurrency limit reached")

type TransferLimiter struct {
	mu        sync.Mutex
	global    int
	perKey    map[string]int
	maxGlobal int
	maxPerKey int
}

func NewTransferLimiter(maxGlobal, maxPerKey int) (*TransferLimiter, error) {
	if maxGlobal < 1 || maxPerKey < 1 || maxPerKey > maxGlobal {
		return nil, errors.New("transfer limits must satisfy 1 <= per-client <= global")
	}
	return &TransferLimiter{perKey: make(map[string]int), maxGlobal: maxGlobal, maxPerKey: maxPerKey}, nil
}

func (l *TransferLimiter) Acquire(key string) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	if key == "" {
		key = "unknown"
	}
	l.mu.Lock()
	if l.global >= l.maxGlobal || l.perKey[key] >= l.maxPerKey {
		l.mu.Unlock()
		return nil, ErrTransferLimit
	}
	l.global++
	l.perKey[key]++
	l.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.global--
			l.perKey[key]--
			if l.perKey[key] == 0 {
				delete(l.perKey, key)
			}
			l.mu.Unlock()
		})
	}, nil
}
