package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// HARNESS

// streamWait is the longest a test waits before it gives up.
const streamWait = 5 * time.Second

// testStream is one open stream that a test can read from and hang up on.
type testStream struct {
	res         *http.Response
	lines       <-chan string   // closed when the stream ends
	handlerDone <-chan struct{} // closed when the server side handler returns
	disconnect  context.CancelFunc
}

// newStreamHandler builds a Handler that talks to the fake and logs to nowhere.
func newStreamHandler(fake *fakeSimulator, shutdownCtx context.Context) *Handler {
	return NewHandler(fake, slog.New(slog.NewJSONHandler(io.Discard, nil)), shutdownCtx)
}

// openStream starts a real server, connects to the stream, and reads its lines into a channel.
func openStream(t *testing.T, h *Handler, query string) *testStream {
	t.Helper()

	handlerDone := make(chan struct{})
	routes := h.Routes()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		routes.ServeHTTP(w, r)
	}))
	ctx, disconnect := context.WithCancel(context.Background())
	t.Cleanup(func() {
		disconnect()
		srv.Close()
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream"+query, nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() { res.Body.Close() })

	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	return &testStream{res: res, lines: lines, handlerDone: handlerDone, disconnect: disconnect}
}

// nextFrame gives back the lines of the next event, which ends at an empty line.
func (s *testStream) nextFrame(t *testing.T) []string {
	t.Helper()
	var frame []string
	timeout := time.After(streamWait)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				t.Fatalf("stream ended before a full frame arrived, got %q", frame)
			}
			if line == "" {
				return frame
			}
			frame = append(frame, line)
		case <-timeout:
			t.Fatalf("timed out waiting for a frame, got %q so far", frame)
		}
	}
}

// waitEnded checks that both the reader and the server handler have stopped.
func (s *testStream) waitEnded(t *testing.T) {
	t.Helper()
	timeout := time.After(streamWait)
	for open := true; open; {
		select {
		case _, open = <-s.lines:
		case <-timeout:
			t.Fatal("timed out waiting for the stream body to end")
		}
	}
	select {
	case <-s.handlerDone:
	case <-timeout:
		t.Fatal("timed out waiting for the stream handler to return")
	}
}

// testSample builds a sample with a fixed time so its JSON is always the same.
func testSample(id chaos.SimulationID, latency float64, breaches ...string) chaos.MetricSample {
	return chaos.MetricSample{
		SimulationID: id,
		Service:      "payments-api",
		Mode:         chaos.FailureLatencySpike,
		Timestamp:    time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
		LatencyP99Ms: latency,
		ErrorRate:    0.02,
		CPUPercent:   40,
		MemoryMB:     512,
		Healthy:      len(breaches) == 0,
		Breaches:     breaches,
	}
}

// TESTS

func TestStreamSendsSamplesAsEvents(t *testing.T) {
	fake := &fakeSimulator{stream: make(chan chaos.MetricSample, 2)}
	fake.stream <- testSample("sim_a", 950, "latency_p99")
	fake.stream <- testSample("sim_a", 130)

	s := openStream(t, newStreamHandler(fake, context.Background()), "")

	if s.res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", s.res.StatusCode, http.StatusOK)
	}
	wantHeaders := map[string]string{
		"Content-Type":  "text/event-stream",
		"Cache-Control": "no-cache",
		"Connection":    "keep-alive",
	}
	for key, want := range wantHeaders {
		if got := s.res.Header.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	wantFrames := [][]string{
		{
			"event: metric",
			`data: {"simulation_id":"sim_a","service":"payments-api","mode":"latency_spike","timestamp":"2026-01-02T15:04:05Z","latency_p99_ms":950,"error_rate":0.02,"cpu_percent":40,"memory_mb":512,"healthy":false,"breaches":["latency_p99"]}`,
		},
		{
			"event: metric",
			`data: {"simulation_id":"sim_a","service":"payments-api","mode":"latency_spike","timestamp":"2026-01-02T15:04:05Z","latency_p99_ms":130,"error_rate":0.02,"cpu_percent":40,"memory_mb":512,"healthy":true,"breaches":[]}`,
		},
	}
	for i, want := range wantFrames {
		if got := s.nextFrame(t); !slices.Equal(got, want) {
			t.Errorf("frame %d = %q, want %q", i, got, want)
		}
	}
}

func TestStreamFiltersBySimulationID(t *testing.T) {
	fake := &fakeSimulator{stream: make(chan chaos.MetricSample, 3)}
	fake.stream <- testSample("sim_a", 111)
	fake.stream <- testSample("sim_b", 222)
	fake.stream <- testSample("sim_a", 333)
	// Closing the channel ends the stream after the three samples are read.
	close(fake.stream)

	s := openStream(t, newStreamHandler(fake, context.Background()), "?simulation_id=sim_b")

	frame := s.nextFrame(t)
	if len(frame) != 2 || !strings.Contains(frame[1], `"simulation_id":"sim_b"`) || !strings.Contains(frame[1], `"latency_p99_ms":222`) {
		t.Errorf("first frame = %q, want only the sim_b sample", frame)
	}

	// The stream must now end with nothing else in it, which proves both sim_a samples were skipped.
	timeout := time.After(streamWait)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				return
			}
			t.Errorf("unexpected extra line after the sim_b frame: %q", line)
		case <-timeout:
			t.Fatal("timed out waiting for the stream to end")
		}
	}
}

func TestStreamSendsHeartbeat(t *testing.T) {
	h := newStreamHandler(&fakeSimulator{}, context.Background())
	h.heartbeat = 10 * time.Millisecond

	s := openStream(t, h, "")

	want := []string{": heartbeat"}
	if got := s.nextFrame(t); !slices.Equal(got, want) {
		t.Errorf("frame = %q, want %q", got, want)
	}
}

func TestStreamEnds(t *testing.T) {
	tests := []struct {
		name string
		end  func(s *testStream, shutdown context.CancelFunc, samples chan chaos.MetricSample)
	}{
		{
			name: "when the server starts shutting down",
			end: func(_ *testStream, shutdown context.CancelFunc, _ chan chaos.MetricSample) {
				shutdown()
			},
		},
		{
			name: "when the subscription channel closes",
			end: func(_ *testStream, _ context.CancelFunc, samples chan chaos.MetricSample) {
				close(samples)
			},
		},
		{
			name: "when the client disconnects",
			end: func(s *testStream, _ context.CancelFunc, _ chan chaos.MetricSample) {
				s.disconnect()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shutdownCtx, shutdown := context.WithCancel(context.Background())
			defer shutdown()
			fake := &fakeSimulator{stream: make(chan chaos.MetricSample, 1)}

			s := openStream(t, newStreamHandler(fake, shutdownCtx), "")

			// Reading one event first proves the stream was really open before we end it.
			fake.stream <- testSample("sim_a", 950)
			if frame := s.nextFrame(t); len(frame) != 2 {
				t.Fatalf("frame = %q, want an event line and a data line", frame)
			}

			tc.end(s, shutdown, fake.stream)
			s.waitEnded(t)
		})
	}
}

func TestStreamSubscribeError(t *testing.T) {
	fake := &fakeSimulator{subscribeErr: errors.New("subscriber table is full at 0xc000123456")}

	s := openStream(t, newStreamHandler(fake, context.Background()), "")

	if s.res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", s.res.StatusCode, http.StatusInternalServerError)
	}
	if got := s.res.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body string
	timeout := time.After(streamWait)
	for open := true; open; {
		var line string
		select {
		case line, open = <-s.lines:
			body += line
		case <-timeout:
			t.Fatal("timed out reading the error body")
		}
	}
	if want := `{"error":"internal server error"}`; body != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}
