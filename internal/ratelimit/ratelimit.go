// Package ratelimit hands out a small number of turns to each caller and slowly gives them back.
package ratelimit

import (
	"sync"
	"time"
)

// LIMITER

// defaultMaxKeys is how many different callers one limiter remembers at most.
const defaultMaxKeys = 10000

// bucket is the jar of turns that belongs to one caller.
type bucket struct {
	tokens  float64
	updated time.Time
}

// Limiter is a token bucket per caller, where every caller is named by a key such as an IP address.
type Limiter struct {
	burst   float64       // how many turns a caller can use in one go
	every   time.Duration // how long it takes to earn one turn back
	maxKeys int
	now     func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

// Option is a little helper that changes one setting when the limiter is built.
type Option func(*Limiter)

// WithClock lets tests give the limiter a pretend clock.
func WithClock(now func() time.Time) Option {
	return func(l *Limiter) { l.now = now }
}

// WithMaxKeys changes how many different callers the limiter remembers at most.
func WithMaxKeys(n int) Option {
	return func(l *Limiter) {
		if n > 0 {
			l.maxKeys = n
		}
	}
}

// New builds a limiter that allows burst turns at once and gives one turn back every interval.
func New(burst int, every time.Duration, opts ...Option) *Limiter {
	if burst < 1 {
		burst = 1
	}
	if every <= 0 {
		every = time.Second
	}
	l := &Limiter{
		burst:   float64(burst),
		every:   every,
		maxKeys: defaultMaxKeys,
		now:     time.Now,
		buckets: make(map[string]*bucket),
	}
	for _, opt := range opts {
		opt(l)
	}
	l.lastSweep = l.now()
	return l
}

// Allow takes one turn from the caller's bucket, or says how long to wait when the bucket is empty.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	b, known := l.buckets[key]
	if !known {
		// A full table means someone is flooding us with new names, so strangers have to wait.
		if len(l.buckets) >= l.maxKeys {
			return false, l.every
		}
		b = &bucket{tokens: l.burst, updated: now}
		l.buckets[key] = b
	}

	// The bucket earns back turns for the time that passed, but never more than a full bucket.
	if elapsed := now.Sub(b.updated); elapsed > 0 {
		b.tokens = min(l.burst, b.tokens+float64(elapsed)/float64(l.every))
		b.updated = now
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) * float64(l.every))
}

// sweepLocked forgets callers whose bucket is full again, and the caller must hold the lock.
func (l *Limiter) sweepLocked(now time.Time) {
	// A bucket is certainly full again after this long, so it equals a brand new one.
	full := time.Duration(l.burst * float64(l.every))
	if now.Sub(l.lastSweep) < full {
		return
	}
	l.lastSweep = now
	for key, b := range l.buckets {
		if now.Sub(b.updated) >= full {
			delete(l.buckets, key)
		}
	}
}

// Len tells us how many callers the limiter remembers right now.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
