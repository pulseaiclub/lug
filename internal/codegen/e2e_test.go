package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/pulseaiclub/lug/internal/parser"
)

// harnessMain is the Go program that hosts the transpiled chunk in E2E
// tests. It also registers the host bindings used by testdata/08_interop.lua.
const harnessMain = `package main

import (
	"fmt"
	"os"

	"github.com/pulseaiclub/lug"
)

type Point struct{ X, Y float64 }

func main() {
	env := lug.New()
	lug.Bind(env, "host_add", func(a, b float64) float64 { return a + b })
	lug.Expose(env, "point", &Point{X: 1, Y: 2})
	lug.Bind(env, "make_point", func(x, y float64) *Point { return &Point{X: x, Y: y} })

	if _, err := lug.Run(env, Chunk_main); err != nil {
		fmt.Fprintln(os.Stderr, "lua error:", err)
		os.Exit(1)
	}
}
`

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repo root")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func goDirective(t *testing.T, gomod string) string {
	t.Helper()
	data, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}
	return "1.21"
}

// TestE2E transpiles every testdata script, compiles it in a temporary Go
// module (with a replace directive to this repo), runs it, and compares
// stdout against the .want golden file. A .wanterr file instead marks the
// script as expected to fail with a substring match on the error output.
func TestE2E(t *testing.T) {
	root := repoRoot(t)
	goVer := goDirective(t, filepath.Join(root, "go.mod"))
	dataDir := filepath.Join(root, "internal", "codegen", "testdata")

	luaFiles, err := filepath.Glob(filepath.Join(dataDir, "*.lua"))
	if err != nil {
		t.Fatal(err)
	}
	if len(luaFiles) == 0 {
		t.Fatal("no testdata scripts found")
	}

	for _, luaFile := range luaFiles {
		name := strings.TrimSuffix(filepath.Base(luaFile), ".lua")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(luaFile)
			if err != nil {
				t.Fatal(err)
			}
			file, err := parser.Parse(src, name+".lua")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			code, err := Generate(file, Options{Package: "main", ChunkName: "Chunk_main", Source: name + ".lua"})
			if err != nil {
				t.Fatalf("generate: %v", err)
			}

			dir := t.TempDir()
			files := map[string]string{
				"go.mod": fmt.Sprintf("module lugtest\n\ngo %s\n\nrequire github.com/pulseaiclub/lug v0.0.0\n\nreplace github.com/pulseaiclub/lug => %s\n",
					goVer, filepath.ToSlash(root)),
				"main.go":      harnessMain,
				"chunk_gen.go": string(code),
			}
			for fname, content := range files {
				if err := os.WriteFile(filepath.Join(dir, fname), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			cmd := exec.Command("go", "run", ".")
			cmd.Dir = dir
			out, runErr := cmd.CombinedOutput()

			wantErrBytes, _ := os.ReadFile(filepath.Join(dataDir, name+".wanterr"))
			wantErr := string(wantErrBytes)

			if wantErr != "" {
				if runErr == nil {
					t.Fatalf("expected failure, got success\noutput:\n%s", out)
				}
				if !strings.Contains(string(out), strings.TrimSpace(wantErr)) {
					t.Fatalf("error output missing %q:\n%s", strings.TrimSpace(wantErr), out)
				}
				return
			}

			if runErr != nil {
				t.Fatalf("go run failed: %v\noutput:\n%s", runErr, out)
			}
			want, err := os.ReadFile(filepath.Join(dataDir, name+".want"))
			if err != nil {
				t.Fatalf("missing .want: %v", err)
			}
			if string(out) != string(want) {
				t.Errorf("output mismatch\n--- want ---\n%s\n--- got ---\n%s", want, out)
			}
		})
	}
}

// TestParseAllTestdata verifies every testdata script parses cleanly.
func TestParseAllTestdata(t *testing.T) {
	root := repoRoot(t)
	luaFiles, _ := filepath.Glob(filepath.Join(root, "internal", "codegen", "testdata", "*.lua"))
	for _, f := range luaFiles {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parser.Parse(src, filepath.Base(f)); err != nil {
			t.Errorf("parse %s: %v", f, err)
		}
	}
}
