package qbitsync

import (
	"time"

	"server/log"
)

const errorLogInterval = 5 * time.Minute

func (s *service) setError(hash string, err error) {
	if err == nil {
		return
	}
	message := err.Error()

	s.mu.Lock()
	s.errors[hash] = message
	loggable := s.logAllowedLocked(hash)
	s.mu.Unlock()

	if loggable {
		log.TLogln("qBittorrent sync error:", hash, message)
	}
}

func (s *service) clearError(hash string) {
	s.mu.Lock()
	delete(s.errors, hash)
	s.mu.Unlock()
}

func (s *service) errorMap() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	errors := make(map[string]string, len(s.errors))
	for hash, message := range s.errors {
		errors[hash] = message
	}
	return errors
}

func (s *service) logProblem(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	loggable := s.logAllowedLocked("")
	s.mu.Unlock()

	if loggable {
		log.TLogln("qBittorrent unavailable:", err)
	}
}

func (s *service) logAllowedLocked(key string) bool {
	now := nowFunc()
	if last, ok := s.loggedAt[key]; ok && now.Sub(last) < errorLogInterval {
		return false
	}
	s.loggedAt[key] = now
	return true
}
