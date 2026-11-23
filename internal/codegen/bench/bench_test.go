// Package bench holds committed samples of transpiled Lua code and
// benchmarks comparing them against hand-written Go, documenting the real
// cost of the transpiled code shape.
package bench

import (
	"testing"

	lg "github.com/pulseaiclub/lug/runtime"
)

// BenchmarkTranspiledFib runs the generated chunk, which computes
// fib(24) with a fully numeric-specialized recursive function.
func BenchmarkTranspiledFib(b *testing.B) {
	env := lg.New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Chunk_fib(env, nil)
	}
}

// BenchmarkGoFib is the hand-written Go baseline for the same computation.
func BenchmarkGoFib(b *testing.B) {
	var fib func(n float64) float64
	fib = func(n float64) float64 {
		if n < 2 {
			return n
		}
		return fib(n-1) + fib(n-2)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fib(24)
	}
}

// BenchmarkTranspiledLoop runs the generated chunk, which sums i%7 over a
// 100k-iteration numeric for loop.
func BenchmarkTranspiledLoop(b *testing.B) {
	env := lg.New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Chunk_loop(env, nil)
	}
}

// BenchmarkGoLoop is the hand-written Go baseline.
func BenchmarkGoLoop(b *testing.B) {
	loop := func(n float64) float64 {
		sum := 0.0
		for i := 1.0; i <= n; i++ {
			sum += float64(int64(i) % 7)
		}
		return sum
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loop(100000)
	}
}
