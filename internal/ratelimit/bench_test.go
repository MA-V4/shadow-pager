package ratelimit

import (
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// BENCHMARKS

// BenchmarkAllowParallel measures Allow when many goroutines ask at the same time.
func BenchmarkAllowParallel(b *testing.B) {
	// Every caller fights over one bucket, which is the worst case for the single lock.
	b.Run("one key", func(b *testing.B) {
		l := New(5, 30*time.Second)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				l.Allow("203.0.113.7")
			}
		})
	})

	// Each goroutine is its own caller, which is closer to real traffic from many addresses.
	b.Run("key per goroutine", func(b *testing.B) {
		l := New(5, 30*time.Second)
		var next atomic.Int64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			key := "203.0.113." + strconv.FormatInt(next.Add(1), 10)
			for pb.Next() {
				l.Allow(key)
			}
		})
	})

	// A flood of brand new names fills the table, and after that every stranger is turned away cheaply.
	b.Run("flood of new keys", func(b *testing.B) {
		l := New(5, 30*time.Second, WithMaxKeys(1000))
		var next atomic.Int64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				l.Allow(strconv.FormatInt(next.Add(1), 10))
			}
		})
		b.StopTimer()

		if got := l.Len(); got > 1000 {
			b.Fatalf("limiter remembers %d callers, want at most 1000", got)
		}
	})
}
