package interp_test

import (
	"testing"

	"github.com/pulseaiclub/lug/internal/codegen/bench"
	"github.com/pulseaiclub/lug/internal/interp"
	lg "github.com/pulseaiclub/lug/runtime"
)

// The sources are identical to internal/codegen/bench/testdata, which keeps the
// interpreted and transpiled numbers comparable.
const (
	fibSrc = `
local function fib(k)
    if k < 2 then return k end
    return fib(k - 1) + fib(k - 2)
end
return fib(24)
`
	loopSrc = `
local function sumloop(n)
    local sum = 0
    for i = 1, n do sum = sum + i % 7 end
    return sum
end
return sumloop(100000)
`
)

func mustProgram(tb testing.TB, src, name string) *interp.Program {
	tb.Helper()
	prog, err := interp.Parse(src, name)
	if err != nil {
		tb.Fatalf("parse: %v", err)
	}
	return prog
}

func BenchmarkInterpretedFib(b *testing.B) {
	prog := mustProgram(b, fibSrc, "=fib")
	env := lg.New()
	fn := prog.Chunk()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(env, nil)
	}
}

func BenchmarkTranspiledFib(b *testing.B) {
	env := lg.New()
	fn := bench.Chunk_fib
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(env, nil)
	}
}

func BenchmarkInterpretedLoop(b *testing.B) {
	prog := mustProgram(b, loopSrc, "=loop")
	env := lg.New()
	fn := prog.Chunk()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(env, nil)
	}
}

func BenchmarkTranspiledLoop(b *testing.B) {
	env := lg.New()
	fn := bench.Chunk_loop
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(env, nil)
	}
}
