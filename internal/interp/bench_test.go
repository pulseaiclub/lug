package interp_test

import (
	"testing"

	"github.com/pulseaiclub/lug/internal/interp"
	lg "github.com/pulseaiclub/lug/runtime"
)

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

func benchChunk(b *testing.B, src, name string) func() {
	b.Helper()
	prog, err := interp.Parse(src, name)
	if err != nil {
		b.Fatalf("parse: %v", err)
	}
	env := lg.New()
	fn := prog.Chunk()
	return func() { fn(env, nil) }
}

// BenchmarkFib is the call-heavy case: every Lua call allocates a scope and an
// argument slice, so it measures dispatch overhead more than arithmetic.
func BenchmarkFib(b *testing.B) {
	run := benchChunk(b, fibSrc, "=fib")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}

// BenchmarkLoop is the arithmetic case: 100k iterations of a numeric for loop
// with one local and a modulo.
func BenchmarkLoop(b *testing.B) {
	run := benchChunk(b, loopSrc, "=loop")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}
