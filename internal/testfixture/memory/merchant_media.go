package memory

import "context"

type merchantMedia struct {
	avatar string
	images []string
}

func (s *Store) GetMerchantMedia(_ context.Context, id string) (string, []string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.media[id]
	return m.avatar, append([]string(nil), m.images...), nil
}
func (s *Store) SaveMerchantMedia(_ context.Context, id, avatar string, images []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.media == nil {
		s.media = map[string]merchantMedia{}
	}
	s.media[id] = merchantMedia{avatar, append([]string(nil), images...)}
	return nil
}
