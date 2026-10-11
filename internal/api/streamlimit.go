package api

import (
	"strconv"
	"sync"
)

// STREAM LIMITS

// streamRetryAfter is how many seconds a caller is asked to wait when every stream slot is taken.
const streamRetryAfter = 10

// StreamLimits says how many live streams may be open at once, and zero means no limit.
type StreamLimits struct {
	MaxTotal int // for all callers together
	MaxPerIP int // for one caller, named by IP address
}

// WithStreamLimits caps how many live streams can be open, so open connections cannot use up the server.
func WithStreamLimits(limits StreamLimits) Option {
	return func(h *Handler) { h.streams = newStreamGate(limits) }
}

// streamGate counts the open streams, in total and for each caller.
type streamGate struct {
	limits StreamLimits

	mu    sync.Mutex
	total int
	perIP map[string]int
}

func newStreamGate(limits StreamLimits) *streamGate {
	return &streamGate{limits: limits, perIP: make(map[string]int)}
}

// enter takes one slot for the caller and gives back the function that hands it back, or false when there is no slot.
func (g *streamGate) enter(key string) (leave func(), ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.limits.MaxTotal > 0 && g.total >= g.limits.MaxTotal {
		return nil, false
	}
	if g.limits.MaxPerIP > 0 && g.perIP[key] >= g.limits.MaxPerIP {
		return nil, false
	}
	g.total++
	g.perIP[key]++

	// The slot can only be handed back once, however often leave is called.
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.total--
			if g.perIP[key]--; g.perIP[key] <= 0 {
				delete(g.perIP, key)
			}
		})
	}, true
}

// open tells us how many streams are open right now, and how many callers they belong to.
func (g *streamGate) open() (total, callers int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.total, len(g.perIP)
}

// retryAfterHeader is the wait we ask for, in the form the header wants.
func retryAfterHeader(seconds int) string { return strconv.Itoa(seconds) }
