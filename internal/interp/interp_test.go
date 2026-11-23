package interp_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pulseaiclub/lug/internal/interp"
	lg "github.com/pulseaiclub/lug/runtime"
)

// Point is the host type testdata/08_interop.lua manipulates.
type Point struct{ X, Y float64 }

// hostEnv builds the same environment the transpiler E2E harness uses, so both
// execution paths can be compared against the same golden files.
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

// TestGoldenScripts interprets every script the transpiler is tested on and
// compares stdout with the same .want golden files. Running the identical
// source through both paths is the strongest check that the interpreter did
// not drift from the transpiled semantics.
func TestGoldenScripts(t *testing.T) {
	dir := filepath.Join("..", "codegen", "testdata")
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
				// The E2E harness prints "lua error: <msg>"; the
				// interpreter returns the message itself.
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
