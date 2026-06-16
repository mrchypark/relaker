package dedupe

import (
	"sync"
	"time"
)

const defaultMemoryStoreTTL = 24 * time.Hour

type MemoryStore struct {
	mu              sync.Mutex
	seen            map[string]time.Time
	ttl             time.Duration
	lastCleanup     time.Time
	cleanupInterval time.Duration
}

func NewMemoryStore() *MemoryStore {
	return NewMemoryStoreWithTTL(defaultMemoryStoreTTL)
}

func NewMemoryStoreWithTTL(ttl time.Duration) *MemoryStore {
	if ttl <= 0 {
		ttl = defaultMemoryStoreTTL
	}
	cleanupInterval := ttl
	if cleanupInterval > time.Minute {
		cleanupInterval = time.Minute
	}
	return &MemoryStore{
		seen:            make(map[string]time.Time),
		ttl:             ttl,
		cleanupInterval: cleanupInterval,
	}
}

func (s *MemoryStore) CheckAndMark(keys []string) (bool, string) {
	if len(keys) == 0 {
		return false, ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.lastCleanup) >= s.cleanupInterval {
		s.deleteExpired(now)
		s.lastCleanup = now
	}
	for _, key := range keys {
		expiresAt, ok := s.seen[key]
		if !ok {
			continue
		}
		if now.Before(expiresAt) {
			return true, key
		}
		delete(s.seen, key)
	}
	expiresAt := now.Add(s.ttl)
	for _, key := range keys {
		s.seen[key] = expiresAt
	}
	return false, ""
}

func (s *MemoryStore) Unmark(keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		delete(s.seen, key)
	}
}

func (s *MemoryStore) deleteExpired(now time.Time) {
	for key, expiresAt := range s.seen {
		if !now.Before(expiresAt) {
			delete(s.seen, key)
		}
	}
}
