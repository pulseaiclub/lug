package codegen

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/lug/internal/parser"
)

// translate parses and transpiles Lua source and returns the Go source.
func translate(t *testing.T, src string) string {
	t.Helper()
	file, err := parser.Parse([]byte(src), "test.lua")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := Generate(file, Options{Package: "main", ChunkName: "Chunk_main", Source: "test.lua"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return string(out)
}

// TestDirectCallSpecialization checks that a local function which is only
// ever called directly is emitted as a fixed-arity Go func (no argument
// slice allocation per call).
func TestDirectCallSpecialization(t *testing.T) {
	src := `
local function greet(a, b) return a .. b end
print(greet("x", "y"))
`
	got := translate(t, src)
	if !strings.Contains(got, "func(*lg.Env, lg.Value, lg.Value) lg.Value") {
		t.Errorf("expected a fixed-arity direct function, got:\n%s", got)
	}
	if strings.Contains(got, `"greet"`) {
		t.Errorf("direct function should not be wrapped in a closure:\n%s", got)
	}
}

// TestNumericSpecialization checks that a fully numeric function is emitted
// with float64 parameters and result, and that its body uses native Go
// arithmetic instead of the Value helpers.
func TestNumericSpecialization(t *testing.T) {
	src := `
local function fib(k)
    if k < 2 then return k end
    return fib(k - 1) + fib(k - 2)
end
return fib(10)
`
	got := translate(t, src)
	if !strings.Contains(got, "func(*lg.Env, float64) float64") {
		t.Errorf("expected a float64 signature, got:\n%s", got)
	}
	for _, helper := range []string{"lg.Add(", "lg.Sub(", "lg.LT("} {
		if strings.Contains(got, helper) {
			t.Errorf("numeric body should not call %s:\n%s", helper, got)
		}
	}
}

// TestNumericForLoop checks that a numeric for loop uses the float64
// iterator and native arithmetic.
func TestNumericForLoop(t *testing.T) {
	src := `
local sum = 0
for i = 1, 10 do sum = sum + i % 7 end
return sum
`
	got := translate(t, src)
	if !strings.Contains(got, "NextNum()") {
		t.Errorf("expected the float64 loop iterator, got:\n%s", got)
	}
	if !strings.Contains(got, "lg.ModNum(") {
		t.Errorf("expected native modulo, got:\n%s", got)
	}
	if !strings.Contains(got, "var sum_2 float64") {
		t.Errorf("expected a float64 accumulator, got:\n%s", got)
	}
}

// TestNoSpecializationForDynamicCode checks that code which is not provably
// numeric or call-only keeps the general Value representation.
func TestNoSpecializationForDynamicCode(t *testing.T) {
	src := `
local function f(a)
    local x = a + 1
    return x
end
local g = f
print(g("5"), f(1))
`
	got := translate(t, src)
	if !strings.Contains(got, "lg.NewClosure") {
		t.Errorf("a function used as a value must stay a closure:\n%s", got)
	}
	if strings.Contains(got, "float64") {
		t.Errorf("nothing here is statically numeric:\n%s", got)
	}
}
