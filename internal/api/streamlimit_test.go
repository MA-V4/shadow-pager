package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// HELPERS

// limitedStreamHandler builds a Handler whose streams are capped and that talks to the fake.
func limitedStreamHandler(fake *fakeSimulator, shutdownCtx context.Context, limits StreamLimits) *Handler {
	return NewHandler(fake, slog.New(slog.NewJSONHandler(io.Discard, nil)), shutdownCtx, WithStreamLimits(limits))
}

// tryStream asks for a stream with a recorder, which is enough for a request that is refused at the door.
func tryStream(h *Handler, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	return rec
}

// openStreams tells us how many streams the handler counts as open.
func openStreams(h *Handler) int {
	total, _ := h.streams.open()
	return total
}

// GATE TESTS

func TestStreamGateCountsPerCallerAndInTotal(t *testing.T) {
	g := newStreamGate(StreamLimits{MaxTotal: 3, MaxPerIP: 2})

	leaveA1, ok := g.enter("a")
	if !ok {
		t.Fatal("first stream of a was refused")
	}
	if _, ok := g.enter("a"); !ok {
		t.Fatal("second stream of a was refused")
	}
	if _, ok := g.enter("a"); ok {
		t.Fatal("third stream of a was let in, want it refused by the per caller limit")
	}
	if _, ok := g.enter("b"); !ok {
		t.Fatal("first stream of b was refused, but b has nothing open")
	}
	if _, ok := g.enter("c"); ok {
		t.Fatal("a fourth stream was let in, want it refused by the total limit")
	}

	leaveA1()
	// Handing a slot back twice must not free a second one.
	leaveA1()
	if total, callers := g.open(); total != 2 || callers != 2 {
		t.Fatalf("open = %d streams of %d callers, want 2 of 2", total, callers)
	}
	if _, ok := g.enter("c"); !ok {
		t.Fatal("c was refused after a slot was handed back")
	}
}

func TestStreamGateForgetsCallersWithNothingOpen(t *testing.T) {
	g := newStreamGate(StreamLimits{MaxTotal: 10, MaxPerIP: 10})
	for _, key := range []string{"a", "b", "c"} {
		leave, ok := g.enter(key)
		if !ok {
			t.Fatalf("stream of %s was refused", key)
		}
		leave()
	}
	if total, callers := g.open(); total != 0 || callers != 0 {
		t.Fatalf("open = %d streams of %d callers, want nothing left, or the map would grow forever", total, callers)
	}
}

func TestStreamGateIsSafeWithManyGoroutines(t *testing.T) {
	g := newStreamGate(StreamLimits{MaxTotal: 8, MaxPerIP: 8})
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if leave, ok := g.enter("a"); ok {
				if total, _ := g.open(); total > 8 {
					t.Errorf("%d streams are open, want at most 8", total)
				}
				leave()
			}
		}()
	}
	wg.Wait()
	if total, callers := g.open(); total != 0 || callers != 0 {
		t.Fatalf("open = %d streams of %d callers after everyone left, want none", total, callers)
	}
}

// HANDLER TESTS

// checkRefusedStream fails the test unless the answer is a 429 that says when to come back.
func checkRefusedStream(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (body %s)", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Retry-After"); got != "10" {
		t.Errorf("Retry-After = %q, want 10", got)
	}
	if want := `{"error":"too many open streams, try again in 10 seconds"}`; strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("body = %s, want %s", rec.Body, want)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want a JSON error and not a stream", got)
	}
}

func TestStreamOverThePerCallerLimitGets429(t *testing.T) {
	fake := &fakeSimulator{stream: make(chan chaos.MetricSample)}
	h := limitedStreamHandler(fake, context.Background(), StreamLimits{MaxTotal: 10, MaxPerIP: 1})
	// The first stream comes through the test server, so its caller is the loopback address.
	first := openStream(t, h, "")

	checkRefusedStream(t, tryStream(h, "127.0.0.1:2000"))
	// A refused request must not take a slot of its own.
	if got := openStreams(h); got != 1 {
		t.Errorf("open streams = %d after a refusal, want still 1", got)
	}

	// Another caller still gets in, which a second test server pretends by giving its requests another address.
	routes := h.Routes()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "198.51.100.7:1000"
		routes.ServeHTTP(w, r)
	}))
	defer other.Close()
	ctx, hangUp := context.WithCancel(context.Background())
	defer hangUp()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, other.URL+"/stream", nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	res, err := other.Client().Do(req)
	if err != nil {
		t.Fatalf("open stream of another caller: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("stream of another caller = %d, want 200", res.StatusCode)
	}
	hangUp()

	first.disconnect()
	first.waitEnded(t)
}

func TestStreamOverTheTotalLimitGets429(t *testing.T) {
	fake := &fakeSimulator{stream: make(chan chaos.MetricSample)}
	h := limitedStreamHandler(fake, context.Background(), StreamLimits{MaxTotal: 1, MaxPerIP: 1})
	first := openStream(t, h, "")

	// This caller has nothing open, and is still refused because every slot is taken.
	checkRefusedStream(t, tryStream(h, "198.51.100.7:1000"))

	first.disconnect()
	first.waitEnded(t)
}

func TestStreamSlotIsReleasedWhenTheCallerDisconnects(t *testing.T) {
	fake := &fakeSimulator{stream: make(chan chaos.MetricSample)}
	h := limitedStreamHandler(fake, context.Background(), StreamLimits{MaxTotal: 1, MaxPerIP: 1})

	first := openStream(t, h, "")
	if rec := tryStream(h, "127.0.0.1:2000"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second stream = %d while the first is open, want 429", rec.Code)
	}

	first.disconnect()
	first.waitEnded(t)

	if got := openStreams(h); got != 0 {
		t.Fatalf("open streams = %d after the caller left, want 0", got)
	}
	// The slot is free again, so the same caller gets a new stream.
	second := openStream(t, h, "")
	if second.res.StatusCode != http.StatusOK {
		t.Fatalf("stream after a disconnect = %d, want 200", second.res.StatusCode)
	}
}

func TestStreamSlotIsReleasedWhenTheServerShutsDown(t *testing.T) {
	shutdownCtx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	fake := &fakeSimulator{stream: make(chan chaos.MetricSample)}
	h := limitedStreamHandler(fake, shutdownCtx, StreamLimits{MaxTotal: 2, MaxPerIP: 2})

	a := openStream(t, h, "")
	b := openStream(t, h, "")
	if got := openStreams(h); got != 2 {
		t.Fatalf("open streams = %d, want 2", got)
	}

	shutdown()
	a.waitEnded(t)
	b.waitEnded(t)

	if got := openStreams(h); got != 0 {
		t.Fatalf("open streams = %d after shutdown, want 0", got)
	}
}

func TestStreamSlotIsReleasedWhenTheSubscriptionEnds(t *testing.T) {
	fake := &fakeSimulator{stream: make(chan chaos.MetricSample)}
	h := limitedStreamHandler(fake, context.Background(), StreamLimits{MaxTotal: 1, MaxPerIP: 1})

	s := openStream(t, h, "")
	close(fake.stream)
	s.waitEnded(t)

	if got := openStreams(h); got != 0 {
		t.Fatalf("open streams = %d after the engine closed the subscription, want 0", got)
	}
}

// panicSimulator blows up when someone subscribes, the way a bug in the stream code would.
type panicSimulator struct{ fakeSimulator }

func (p *panicSimulator) Subscribe(context.Context) (<-chan chaos.MetricSample, error) {
	panic("boom")
}

func TestStreamSlotIsReleasedWhenTheHandlerPanics(t *testing.T) {
	h := NewHandler(&panicSimulator{}, slog.New(slog.NewJSONHandler(io.Discard, nil)), context.Background(),
		WithStreamLimits(StreamLimits{MaxTotal: 1, MaxPerIP: 1}))
	routes := h.Routes()

	// The real server has a recover middleware in front, and this handler stands in for it.
	panics := make(chan any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			panics <- recover()
			w.WriteHeader(http.StatusInternalServerError)
		}()
		routes.ServeHTTP(w, r)
	}))
	defer srv.Close()

	for i := 0; i < 3; i++ {
		res, err := srv.Client().Get(srv.URL + "/stream")
		if err != nil {
			t.Fatalf("GET /stream: %v", err)
		}
		res.Body.Close()
		// With a leaked slot the second round would be refused at the door and would never get to panic.
		if got := <-panics; got == nil {
			t.Fatalf("round %d did not panic (status %d), so a slot was not handed back", i+1, res.StatusCode)
		}
		if got := openStreams(h); got != 0 {
			t.Fatalf("open streams = %d after panic %d, want 0", got, i+1)
		}
	}
}

func TestStreamErrorsBeforeTheStreamStartsReleaseTheSlot(t *testing.T) {
	fake := &fakeSimulator{subscribeErr: context.DeadlineExceeded}
	h := limitedStreamHandler(fake, context.Background(), StreamLimits{MaxTotal: 1, MaxPerIP: 1})

	for i := 0; i < 3; i++ {
		srv := httptest.NewServer(h.Routes())
		res, err := srv.Client().Get(srv.URL + "/stream")
		if err != nil {
			t.Fatalf("GET /stream: %v", err)
		}
		res.Body.Close()
		srv.Close()
		if res.StatusCode != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 from the failing subscription and never 429", res.StatusCode)
		}
	}
	if got := openStreams(h); got != 0 {
		t.Fatalf("open streams = %d, want 0", got)
	}
}

func TestStreamsAreUnlimitedWithoutTheOption(t *testing.T) {
	fake := &fakeSimulator{stream: make(chan chaos.MetricSample)}
	h := newStreamHandler(fake, context.Background())

	for i := 0; i < 8; i++ {
		if s := openStream(t, h, ""); s.res.StatusCode != http.StatusOK {
			t.Fatalf("stream %d = %d, want 200", i+1, s.res.StatusCode)
		}
	}
}
