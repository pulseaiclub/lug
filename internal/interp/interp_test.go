package interp_test

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pulseaiclub/lug/internal/interp"
	lg "github.com/pulseaiclub/lug/runtime"
)

// Point is the host type testdata/08_interop.lua manipulates.
type Point struct{ X, Y float64 }

// hostEnv builds the environment testdata/08_interop.lua expects: a bound Go
// function and an exposed Go struct.
func hostEnv() *lg.Env {
	env := lg.New()
	env.Set("host_add", lg.WrapFunc(func(a, b float64) float64 { return a + b }))
	env.Set("point", lg.WrapValue(&Point{X: 1, Y: 2}))
	env.Set("make_point", lg.WrapFunc(func(x, y float64) *Point { return &Point{X: x, Y: y} }))
	return env
}

// captureStdout runs fn with os.Stdout redirected into a pipe, which is how
// Lua's print reaches the outside world.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		io.Copy(&sb, r)
		done <- sb.String()
	}()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

// TestGoldenScripts interprets every golden script and compares stdout with
// its .want file (or expects the .wanterr message). The cases cover the
// language corners that are easy to get wrong: closures and shared upvalues,
// metatables, varargs, multiple assignment order, goto, coroutines, error
// propagation and the standard library.
func TestGoldenScripts(t *testing.T) {
	dir := "testdata"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	ran := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".lua") {
			continue
		}
		base := strings.TrimSuffix(name, ".lua")
		wantErrPath := filepath.Join(dir, base+".wanterr")
		wantPath := filepath.Join(dir, base+".want")
		wantErrBytes, errErr := os.ReadFile(wantErrPath)
		wantBytes, errWant := os.ReadFile(wantPath)

		t.Run(base, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read script: %v", err)
			}
			prog, err := interp.Parse(string(src), base)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			env := hostEnv()
			var got string
			var runErr error
			got = captureStdout(t, func() {
				_, runErr = prog.Run(env)
			})
			ran++

			switch {
			case errErr == nil:
				// The script is expected to fail with a known message.
				// Golden files may carry the old harness prefix; compare
				// against the message itself.
				want := strings.TrimSpace(string(wantErrBytes))
				want = strings.TrimPrefix(want, "lua error: ")
				if runErr == nil {
					t.Fatalf("expected an error containing %q, got none (stdout %q)", want, got)
				}
				if !strings.Contains(runErr.Error(), want) {
					t.Fatalf("error = %q, want it to contain %q", runErr.Error(), want)
				}
			case errWant == nil:
				if runErr != nil {
					t.Fatalf("unexpected error: %v", runErr)
				}
				if got != string(wantBytes) {
					t.Fatalf("stdout mismatch\n got: %q\nwant: %q", got, string(wantBytes))
				}
			default:
				if runErr != nil {
					t.Fatalf("unexpected error: %v", runErr)
				}
			}
		})
	}
	if ran == 0 {
		t.Fatal("no scripts were interpreted")
	}
}

// TestLoopScopes covers the two loop paths the interpreter takes: a body
// without closures reuses one binding per loop, a body with closures needs a
// fresh binding per iteration (Lua 5.4 closes upvalues at the end of the
// iteration, so each closure sees its own value).
func TestLoopScopes(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"no closure", `
			local s = 0
			for i = 1, 3 do
				local d = i * 2
				s = s + d
			end
			return s`, "12"},
		{"captured local", `
			local f = {}
			for i = 1, 3 do
				local k = i * i
				f[i] = function() return k end
			end
			return f[1]() + f[2]() + f[3]()`, "14"},
		{"captured control variable", `
			local f = {}
			for i = 1, 3 do f[i] = function() return i end end
			return f[1]() * 100 + f[3]()`, "103"},
		{"shadowing the control variable", `
			local out = ""
			for i = 1, 2 do
				local i = i * 10
				out = out .. i .. ","
			end
			return out`, "10,20,"},
		{"generic for", `
			local total = 0
			for _, v in ipairs({10, 20, 30}) do total = total + v end
			return total`, "60"},
		{"nested loops", `
			local n = 0
			for i = 1, 3 do
				for j = 1, 3 do n = n + 1 end
			end
			return n`, "9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := lg.New()
			prog, err := interp.Parse(c.src, "=case")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			rets, err := prog.Run(env)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			var got string
			switch v := rets[0]; {
			case v.IsString():
				got = v.Str()
			default:
				f, _ := lg.ToNumber(v)
				got = strconv.FormatFloat(f, 'g', -1, 64)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
