package dedupe

import "sync"

type MemoryStore struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{seen: make(map[string]struct{})}
}

func (s *MemoryStore) CheckAndMark(keys []string) (bool, string) {
	if len(keys) == 0 {
		return false, ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		if _, ok := s.seen[key]; ok {
			return true, key
		}
	}
	for _, key := range keys {
		s.seen[key] = struct{}{}
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
