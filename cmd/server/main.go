// Command server boots the Shadow-Pager API: it loads configuration, wires the
// chaos engine and HTTP router together, and owns the process lifecycle from
// startup through graceful shutdown.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/MA-V4/shadow-pager/internal/api"
	"github.com/MA-V4/shadow-pager/internal/chaos"
)

// version is stamped at build time:
//
//	go build -ldflags "-X main.version=$(git rev-parse --short HEAD)" ./cmd/server
var version = "dev"

func main() {
	if err := run(context.Background(), os.Getenv, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "shadow-pager: %v\n", err)
		os.Exit(1)
	}
}

// CONFIG

// config holds every tunable the process reads at startup. Values come from
// the environment so the same binary runs locally and on Render unchanged.
type config struct {
	Host            string
	Port            string
	Env             string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	TickInterval    time.Duration
	MaxActiveSims   int
}

// loadConfig is a pure function of its input, so it takes no context.
// Injecting getenv instead of calling os.Getenv keeps it trivially testable.
func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		Host:            getenv("HOST"),                // empty means listen on every network, which Render needs
		Port:            envOr(getenv, "PORT", "8080"), // Render injects PORT
		Env:             envOr(getenv, "APP_ENV", "development"),
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: 20 * time.Second,
		TickInterval:    time.Second,
		MaxActiveSims:   10,
	}

	if raw := getenv("LOG_LEVEL"); raw != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(raw)); err != nil {
			return config{}, fmt.Errorf("parse LOG_LEVEL %q: %w", raw, err)
		}
	}

	durations := []struct {
		key string
		dst *time.Duration
	}{
		{"SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout},
		{"CHAOS_TICK_INTERVAL", &cfg.TickInterval},
	}
	for _, d := range durations {
		raw := getenv(d.key)
		if raw == "" {
			continue
		}
		v, err := time.ParseDuration(raw)
		if err != nil {
			return config{}, fmt.Errorf("parse %s %q: %w", d.key, raw, err)
		}
		*d.dst = v
	}

	if raw := getenv("CHAOS_MAX_ACTIVE"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return config{}, fmt.Errorf("parse CHAOS_MAX_ACTIVE %q: %w", raw, err)
		}
		cfg.MaxActiveSims = v
	}

	return cfg, nil
}

func envOr(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

// LIFECYCLE

// run is the real main. It returns an error instead of calling os.Exit so
// deferred cleanup always executes and the whole boot path can be tested.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig(getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{
		Level:       cfg.LogLevel,
		ReplaceAttr: humanDurations,
	})).
		With(slog.String("service", "shadow-pager"), slog.String("version", version), slog.String("env", cfg.Env))
	slog.SetDefault(logger)

	engine, err := chaos.NewEngine(chaos.Config{
		TickInterval: cfg.TickInterval,
		MaxActive:    cfg.MaxActiveSims,
	}, logger, chaos.WithDefaultGenerators())
	if err != nil {
		return fmt.Errorf("build chaos engine: %w", err)
	}

	// draining flips to true the moment shutdown begins, so /readyz starts
	// failing and the platform stops routing new traffic to this instance.
	var draining atomic.Bool

	// Open streams watch this context so they hang up when the server starts to turn off.
	streamCtx, cancelStreams := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelStreams()

	srv := &http.Server{
		Handler:           newRouter(logger, engine, api.NewHandler(engine, logger, streamCtx), &draining),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// Streaming handlers (Phase 3) extend their own deadline through
		// http.ResponseController rather than disabling this globally.
		WriteTimeout:   15 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20,
		ErrorLog:       slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	srv.RegisterOnShutdown(cancelStreams)

	// Binding before serving surfaces "address already in use" synchronously,
	// so we never log "listening" for a server that is not.
	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	// The engine deliberately does NOT inherit the signal context. On SIGTERM
	// we drain HTTP first and stop the engine second, so in-flight requests
	// never land on an engine that has already stopped ticking.
	engineCtx, cancelEngine := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelEngine()

	engineDone := make(chan error, 1)
	go func() { engineDone <- engine.Run(engineCtx) }()

	serverErr := make(chan error, 1)
	go func() {
		logger.InfoContext(ctx, "http server listening", slog.String("addr", ln.Addr().String()))
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Block until a signal arrives or a component dies on its own.
	var runErr error
	engineStopped := false
	select {
	case <-ctx.Done():
		logger.InfoContext(ctx, "shutdown signal received")
	case err := <-serverErr:
		runErr = fmt.Errorf("http server: %w", err)
	case err := <-engineDone:
		engineStopped = true
		runErr = fmt.Errorf("chaos engine stopped unexpectedly: %w", err)
	}

	return errors.Join(runErr, shutdown(ctx, logger, cfg, srv, &draining, cancelEngine, engineDone, engineStopped))
}

// shutdown drains HTTP traffic and stops the engine within cfg.ShutdownTimeout.
// Keep that timeout below the platform's kill window (Render's default grace
// period is 30s) so we exit cleanly instead of being SIGKILLed mid-drain.
func shutdown(
	ctx context.Context,
	logger *slog.Logger,
	cfg config,
	srv *http.Server,
	draining *atomic.Bool,
	cancelEngine context.CancelFunc,
	engineDone <-chan error,
	engineStopped bool,
) error {
	draining.Store(true)

	// ctx is already cancelled here, so derive a fresh deadline from it with
	// WithoutCancel: logging values survive, cancellation does not.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()

	logger.InfoContext(shutdownCtx, "draining http server", slog.Duration("timeout", cfg.ShutdownTimeout))
	var errs []error
	if err := srv.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("http shutdown: %w", err))
	}

	cancelEngine()
	if !engineStopped {
		select {
		case err := <-engineDone:
			if err != nil {
				errs = append(errs, fmt.Errorf("chaos engine shutdown: %w", err))
			}
		case <-shutdownCtx.Done():
			errs = append(errs, fmt.Errorf("chaos engine shutdown: %w", shutdownCtx.Err()))
		}
	}

	logger.InfoContext(shutdownCtx, "shutdown complete")
	return errors.Join(errs...)
}

// HTTP

func newRouter(logger *slog.Logger, engine *chaos.Engine, apiHandler *api.Handler, draining *atomic.Bool) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestLogger(logger))
	r.Use(middleware.Recoverer)

	started := time.Now()

	// Liveness: is the process able to serve at all? Never checks
	// dependencies, otherwise a database blip gets the pod restarted.
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(r.Context(), logger, w, http.StatusOK, map[string]any{
			"status":  "ok",
			"version": version,
			"uptime":  time.Since(started).Round(time.Second).String(),
		})
	})

	// Readiness: should traffic be routed here right now?
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		checks := map[string]string{"engine": "ok", "lifecycle": "ok"}
		status := http.StatusOK

		if !engine.Ready() {
			checks["engine"] = "not running"
			status = http.StatusServiceUnavailable
		}
		if draining.Load() {
			checks["lifecycle"] = "draining"
			status = http.StatusServiceUnavailable
		}

		writeJSON(r.Context(), logger, w, status, map[string]any{"checks": checks})
	})

	r.Mount("/api/v1", apiHandler.Routes())

	return r
}

// requestLogger emits one structured line per request, correlated by the
// request ID that middleware.RequestID placed on the context.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			next.ServeHTTP(ww, r)

			logger.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("request_id", middleware.GetReqID(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				// Numeric milliseconds, so log queries can filter and aggregate.
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			)
		})
	}
}

// humanDurations renders time.Duration attrs as "20s" instead of the JSON
// handler's default raw nanoseconds (20000000000).
func humanDurations(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindDuration {
		return slog.String(a.Key, a.Value.Duration().String())
	}
	return a
}

func writeJSON(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Headers are already sent, so logging is all we can do.
		logger.ErrorContext(ctx, "encode response", slog.Any("error", err))
	}
}
