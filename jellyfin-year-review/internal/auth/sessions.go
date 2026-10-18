// Package auth validates online or exported credentials and keeps opaque sessions in server memory.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"reelreview/internal/source"
)

type Session struct {
	Account source.Account
	Token   string
	Device  string
	Expires time.Time
}
type Store struct {
	mu       sync.Mutex
	sessions map[[32]byte]Session
	Now      func() time.Time
	TTL      time.Duration
}

func New() *Store {
	return &Store{sessions: map[[32]byte]Session{}, Now: time.Now, TTL: 12 * time.Hour}
}
func Random() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("system randomness unavailable")
	}
	return hex.EncodeToString(b[:])
}
func (s *Store) Create(a source.Account, token, device string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for k, v := range s.sessions {
		if !now.Before(v.Expires) {
			delete(s.sessions, k)
		}
	}
	if len(s.sessions) >= 1000 {
		return "", false
	}
	id := Random()
	s.sessions[sha256.Sum256([]byte(id))] = Session{Account: a, Token: token, Device: device, Expires: now.Add(s.TTL)}
	return id, true
}
func (s *Store) Get(id string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := sha256.Sum256([]byte(id))
	v, ok := s.sessions[key]
	if len(id) != 64 || !ok || !s.Now().Before(v.Expires) {
		delete(s.sessions, key)
		return Session{}, false
	}
	return v, true
}
func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sha256.Sum256([]byte(id)))
}

type bucket struct {
	Count int
	Reset time.Time
}
type Limiter struct {
	mu      sync.Mutex
	entries map[string]bucket
	Now     func() time.Time
}

func NewLimiter() *Limiter { return &Limiter{entries: map[string]bucket{}, Now: time.Now} }

// Allow uses socket peer IP, never untrusted forwarded headers. A bounded global
// bucket also limits distributed login traffic and upstream authentication load.
func (l *Limiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	for k, v := range l.entries {
		if !now.Before(v.Reset) {
			delete(l.entries, k)
		}
	}
	if len(l.entries) >= 4096 {
		return false
	}
	for _, key := range []string{"global", ip} {
		v := l.entries[key]
		if v.Reset.IsZero() {
			v.Reset = now.Add(time.Minute)
		}
		limit := 10
		if key == "global" {
			limit = 60
		}
		if v.Count >= limit {
			return false
		}
		v.Count++
		l.entries[key] = v
	}
	return true
}
