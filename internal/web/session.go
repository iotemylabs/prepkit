package web

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// sessionStore is an in-memory table of opaque session tokens. Sessions do
// not survive a restart, which is acceptable for a single-admin instance:
// logging in again is cheap, and nothing secret persists to disk.
type sessionStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time // token -> expiry
	ttl    time.Duration
}

func newSessionStore(ttl time.Duration) *sessionStore {
	return &sessionStore{tokens: map[string]time.Time{}, ttl: ttl}
}

func (s *sessionStore) create() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("web: reading random source: " + err.Error())
	}
	token := hex.EncodeToString(b[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = time.Now().Add(s.ttl)
	return token
}

// valid reports whether the token names a live session, sliding its expiry
// forward on use and pruning dead tokens as a side effect.
func (s *sessionStore) valid(token string) bool {
	if token == "" {
		return false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for t, exp := range s.tokens {
		if exp.Before(now) {
			delete(s.tokens, t)
		}
	}
	if _, ok := s.tokens[token]; !ok {
		return false
	}
	s.tokens[token] = now.Add(s.ttl)
	return true
}

func (s *sessionStore) revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
}
