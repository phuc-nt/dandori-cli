package intent

import (
	"os"
	"testing"
)

// BenchmarkWalkSmall measures JSONL streaming performance on a small fixture
// (~1MB) that fits comfortably in memory. Tests the fundamental streaming
// path without allocation bloat.
//
// Post quick-win #3 (streaming JSONL parse):
//   - Should use bufio.Scanner (already implemented in Walk)
//   - Memory profile should show constant RSS, not spike to 3× input size
func BenchmarkWalkSmall(b *testing.B) {
	path := "testdata/bench_session_1mb.jsonl"
	if _, err := os.Stat(path); err != nil {
		b.Fatalf("fixture %s not found: %v", path, err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		count := 0
		err := Walk(path, func(line parsedLine) {
			count++
		})
		if err != nil {
			b.Fatalf("Walk failed: %v", err)
		}
	}
}

// BenchmarkWalkLarge measures streaming performance on a realistic 90MB JSONL
// session file. This is the actual production bottleneck — ensures memory
// usage stays constant (no full-file buffers) and throughput is acceptable
// (goal: <2 sec for 90MB session).
//
// Set DANDORI_BENCH_JSONL_LARGE env var to path of 90MB .jsonl session.
// If not set, benchmark is skipped (file too large to include in repo).
//
// Run with:
//   DANDORI_BENCH_JSONL_LARGE=/path/to/90mb.jsonl \
//   go test -bench=BenchmarkWalkLarge -benchmem -benchtime=3s -run=^$ ./internal/intent/
func BenchmarkWalkLarge(b *testing.B) {
	path := os.Getenv("DANDORI_BENCH_JSONL_LARGE")
	if path == "" {
		b.Skip("set DANDORI_BENCH_JSONL_LARGE to a large session jsonl (90MB+)")
	}

	if _, err := os.Stat(path); err != nil {
		b.Fatalf("DANDORI_BENCH_JSONL_LARGE file not found: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		count := 0
		err := Walk(path, func(line parsedLine) {
			count++
		})
		if err != nil {
			b.Fatalf("Walk failed: %v", err)
		}
	}
}

// BenchmarkWalkParallel tests the concurrent read safety of Walk.
// Multiple goroutines reading the same file should not cause races or
// memory contention (streaming is per-goroutine, no shared state).
func BenchmarkWalkParallel(b *testing.B) {
	path := "testdata/bench_session_1mb.jsonl"
	if _, err := os.Stat(path); err != nil {
		b.Fatalf("fixture %s not found: %v", path, err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			count := 0
			err := Walk(path, func(line parsedLine) {
				count++
			})
			if err != nil {
				b.Fatalf("Walk failed: %v", err)
			}
		}
	})
}

// BenchmarkWalkMalformed measures the path through skip logic when encountering
// malformed lines. Should be negligible overhead — debug log, continue.
func BenchmarkWalkMalformed(b *testing.B) {
	path := "testdata/malformed.jsonl"
	if _, err := os.Stat(path); err != nil {
		b.Fatalf("fixture %s not found: %v", path, err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		count := 0
		err := Walk(path, func(line parsedLine) {
			count++
		})
		if err != nil {
			b.Fatalf("Walk failed: %v", err)
		}
	}
}
