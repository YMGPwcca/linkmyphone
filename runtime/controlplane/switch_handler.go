package controlplane

import "sync"

type SwitchHandler struct {
	mu      sync.RWMutex
	handler Handler
}

func NewSwitchHandler(initial Handler) *SwitchHandler {
	return &SwitchHandler{handler: initial}
}

func (s *SwitchHandler) Set(handler Handler) {
	s.mu.Lock()
	s.handler = handler
	s.mu.Unlock()
}

func (s *SwitchHandler) HandleControl(request Request) Response {
	s.mu.RLock()
	defer s.mu.RUnlock()
	handler := s.handler
	if handler == nil {
		return Failure(ErrUnavailable)
	}
	return handler.HandleControl(request)
}
