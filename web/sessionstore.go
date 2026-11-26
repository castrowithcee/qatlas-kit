package web

import (
	"sync"
	"time"
)

// SessionRecord is what a SessionStore keeps per session.
type SessionRecord struct {
	// Created is when the session was issued.
	Created time.Time
	// Expires is the instant after which the session is no longer valid. A store may drop the record then.
	Expires time.Time
}

// SessionStore keeps sessions on the server. The key is derived from the session ID with a hash, so a
// store never holds an ID a browser could present; implementations must be safe for concurrent use.
type SessionStore interface {
	// Put creates or replaces the record under key. It is for issuing a session, never for renewing one.
	Put(key string, rec SessionRecord) error
	// Touch sets Expires of the record under key and reports whether there was one. It must be atomic
	// and never create a record, so a session ended meanwhile stays ended.
	Touch(key string, expires time.Time) (ok bool, err error)
	// Get returns the record under key; ok is false if there is none.
	Get(key string) (rec SessionRecord, ok bool, err error)
	// Delete removes the record under key; a missing key is not an error.
	Delete(key string) error
}

// MemorySessionStore is a SessionStore in process memory. Sessions end with the process.
type MemorySessionStore struct {
	mu   sync.Mutex
	recs map[string]SessionRecord
	now  func() time.Time
	next time.Time
}

// NewMemorySessionStore returns an empty MemorySessionStore.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{recs: map[string]SessionRecord{}, now: time.Now}
}

func (m *MemorySessionStore) Put(key string, rec SessionRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Abandoned sessions are never looked up again, so expired ones are dropped here, at most once a minute.
	if now := m.now(); now.After(m.next) {
		for k, r := range m.recs {
			if now.After(r.Expires) {
				delete(m.recs, k)
			}
		}
		m.next = now.Add(time.Minute)
	}
	m.recs[key] = rec
	return nil
}

func (m *MemorySessionStore) Get(key string) (SessionRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.recs[key]
	return rec, ok, nil
}

func (m *MemorySessionStore) Touch(key string, expires time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.recs[key]
	if ok {
		rec.Expires = expires
		m.recs[key] = rec
	}
	return ok, nil
}

func (m *MemorySessionStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.recs, key)
	return nil
}
