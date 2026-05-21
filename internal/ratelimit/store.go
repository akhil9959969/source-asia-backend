// Package ratelimit implements a sliding-window rate limiter.
// Each user is allowed at most 5 accepted requests per rolling 60-second window.
// All state is held in memory; a server restart clears all counters.
package ratelimit

import (
	"sync"
	"time"
)

const (
	WindowDuration = time.Minute // rolling window length
	MaxRequests    = 5           // accepted requests allowed per window
)

// userState holds per-user rate-limit data.
type userState struct {
	// accepted stores timestamps of accepted requests still within the window.
	// Entries are always appended in ascending order; stale ones are pruned lazily.
	accepted []time.Time

	// rejectedTotal is a cumulative all-time count of rejected requests.
	// It never resets, even when a new window starts.
	rejectedTotal int64
}

// StatsSnapshot is a point-in-time view of one user's counters.
type StatsSnapshot struct {
	AcceptedInWindow int   `json:"accepted_in_window"` // requests accepted in the last 60 s
	RejectedTotal    int64 `json:"rejected_total"`     // all-time rejected count
}

// Store is a concurrency-safe in-memory store for rate-limit state.
//
// A single sync.Mutex serialises all reads and writes.  This is correct for
// any number of goroutines.  In a high-throughput production deployment you
// would shard the map by a hash of user_id so concurrent users rarely
// contend on the same lock.
type Store struct {
	mu    sync.Mutex
	users map[string]*userState
}

// NewStore returns an empty, ready-to-use Store.
func NewStore() *Store {
	return &Store{users: make(map[string]*userState)}
}

// getOrCreate returns the state for userID, creating it if absent.
// Caller must hold s.mu.
func (s *Store) getOrCreate(userID string) *userState {
	st, ok := s.users[userID]
	if !ok {
		st = &userState{}
		s.users[userID] = st
	}
	return st
}

// purgeOld removes timestamps that have fallen outside the rolling window.
// Timestamps are stored in ascending order so we scan from the front.
// Caller must hold s.mu.
func purgeOld(st *userState, now time.Time) {
	cutoff := now.Add(-WindowDuration)
	i := 0
	for i < len(st.accepted) && st.accepted[i].Before(cutoff) {
		i++
	}
	st.accepted = st.accepted[i:]
}

// TryAccept attempts to record an accepted request for userID.
// Returns true if accepted, false if the rate limit is exceeded.
// Safe to call concurrently from any number of goroutines.
func (s *Store) TryAccept(userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	st := s.getOrCreate(userID)
	purgeOld(st, now)

	if len(st.accepted) >= MaxRequests {
		st.rejectedTotal++
		return false
	}

	st.accepted = append(st.accepted, now)
	return true
}

// GetStats returns a per-user snapshot of all known users.
// It prunes stale timestamps before sampling so AcceptedInWindow is current.
func (s *Store) GetStats() map[string]StatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	out := make(map[string]StatsSnapshot, len(s.users))
	for uid, st := range s.users {
		purgeOld(st, now)
		out[uid] = StatsSnapshot{
			AcceptedInWindow: len(st.accepted),
			RejectedTotal:    st.rejectedTotal,
		}
	}
	return out
}
