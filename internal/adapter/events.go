package adapter

import "sync"

type eventStore struct {
	mu    sync.Mutex
	limit int
	items []event
}

func newEventStore(limit int) *eventStore {
	return &eventStore{limit: limit}
}

func (s *eventStore) Add(item event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append([]event{item}, s.items...)
	if s.limit > 0 && len(s.items) > s.limit {
		s.items = s.items[:s.limit]
	}
}

func (s *eventStore) List() []event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]event, len(s.items))
	copy(out, s.items)
	return out
}
