package dedupe

import (
	"sync"
	"time"
)

const defaultMemoryStoreTTL = 24 * time.Hour

type MemoryStore struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

func NewMemoryStore() *MemoryStore {
	return NewMemoryStoreWithTTL(defaultMemoryStoreTTL)
}

func NewMemoryStoreWithTTL(ttl time.Duration) *MemoryStore {
	if ttl <= 0 {
		ttl = defaultMemoryStoreTTL
	}
	return &MemoryStore{
		seen: make(map[string]time.Time),
		ttl:  ttl,
	}
}

func (s *MemoryStore) CheckAndMark(keys []string) (bool, string) {
	if len(keys) == 0 {
		return false, ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.deleteExpired(now)
	for _, key := range keys {
		if _, ok := s.seen[key]; ok {
			return true, key
		}
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
