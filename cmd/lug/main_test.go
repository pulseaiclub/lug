package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// capture runs the CLI with argv and returns what it printed to stdout.
func capture(t *testing.T, argv ...string) (string, error) {
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
	runErr := run(argv)
	w.Close()
	os.Stdout = old
	return <-done, runErr
}

func TestRunFileWithArgs(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "args.lua")
	body := `
print(...)
print(arg[0], arg[1], arg[2])
return nil
`
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := capture(t, script, "one", "two")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "one\ttwo\n" + script + "\tone\ttwo\n"
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

func TestRunCode(t *testing.T) {
	out, err := capture(t, "-e", `print(2 ^ 10, ("abc"):upper())`, "x")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "1024\tABC\n" {
		t.Fatalf("output = %q", out)
	}

	// The chunk sees the extra arguments too.
	out, err = capture(t, "-e", "print(...)", "a", "b")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "a\tb\n" {
		t.Fatalf("output = %q", out)
	}
}

func TestRunError(t *testing.T) {
	if _, err := capture(t, "-e", "local x; return x.field"); err == nil {
		t.Fatal("a runtime error must be reported")
	}
	if _, err := capture(t, "-e", "return +"); err == nil {
		t.Fatal("a syntax error must be reported")
	}
	if _, err := capture(t, "/nonexistent/file.lua"); err == nil {
		t.Fatal("a missing file must be reported")
	}
}
