package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

const sessionCookieName = "aegiscore_smc_session"

type operatorSession struct {
	Username  string
	ExpiresAt time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[[sha256.Size]byte]operatorSession
	ttl      time.Duration
	now      func() time.Time
}

func newSessionStore(ttl time.Duration) *sessionStore {
	return &sessionStore{sessions: make(map[[sha256.Size]byte]operatorSession), ttl: ttl, now: time.Now}
}

func (s *sessionStore) create(username string) (string, operatorSession, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", operatorSession{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	session := operatorSession{Username: username, ExpiresAt: s.now().Add(s.ttl)}
	s.mu.Lock()
	s.pruneLocked()
	s.sessions[sha256.Sum256([]byte(token))] = session
	s.mu.Unlock()
	return token, session, nil
}

func (s *sessionStore) get(token string) (operatorSession, bool) {
	if token == "" {
		return operatorSession{}, false
	}
	key := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[key]
	if !ok || !session.ExpiresAt.After(s.now()) {
		delete(s.sessions, key)
		return operatorSession{}, false
	}
	return session, true
}

func (s *sessionStore) delete(token string) {
	key := sha256.Sum256([]byte(token))
	s.mu.Lock()
	delete(s.sessions, key)
	s.mu.Unlock()
}

func (s *sessionStore) pruneLocked() {
	now := s.now()
	for key, session := range s.sessions {
		if !session.ExpiresAt.After(now) {
			delete(s.sessions, key)
		}
	}
}
