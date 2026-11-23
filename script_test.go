package lug_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pulseaiclub/lug"
)

// TestDoStringBasics covers the run-time path: source that only exists at run
// time is parsed, then interpreted, and its results come back as values.
func TestDoStringBasics(t *testing.T) {
	env := lug.New()

	rets, err := lug.DoString(env, "return 1 + 2, 'ok'")
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	if n, _ := lug.As[float64](rets[0]); n != 3 {
		t.Fatalf("first result = %v, want 3", n)
	}
	if s, _ := lug.As[string](rets[1]); s != "ok" {
		t.Fatalf("second result = %q, want ok", s)
	}

	// Values stay in the host environment, so later scripts see them.
	env.Set("host", lug.ValueOf("from go"))
	rets, err = lug.DoString(env, "return host .. '!'")
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	if s, _ := lug.As[string](rets[0]); s != "from go!" {
		t.Fatalf("second script saw %q", s)
	}
}

// TestScriptReuse runs one compiled script many times, including concurrently,
// to prove each invocation gets a fresh scope.
func TestScriptReuse(t *testing.T) {
	env := lug.New()
	s, err := lug.Compile(`
		runs = (runs or 0) + 1
		return runs, ...
	`, "=reuse")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for i := 1; i <= 3; i++ {
		rets, err := s.Run(env, lug.String("arg"))
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if n, _ := lug.As[float64](rets[0]); n != float64(i) {
			t.Fatalf("run %d: counter = %v, want %d (globals must persist)", i, n, i)
		}
		if a, _ := lug.As[string](rets[1]); a != "arg" {
			t.Fatalf("run %d: vararg = %q, want arg", i, a)
		}
	}

	// A script without shared state may run concurrently: parsing is done and
	// each invocation gets its own scope.
	pure, err := lug.Compile(`local n = ...; return n * n`, "=pure")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rets, err := pure.Run(env, lug.Number(float64(i)))
			if err != nil {
				t.Errorf("concurrent run: %v", err)
				return
			}
			if n, _ := lug.As[float64](rets[0]); n != float64(i*i) {
				t.Errorf("concurrent run %d = %v", i, n)
			}
		}(i)
	}
	wg.Wait()
}

// TestScriptCallsGoAndGoCallsScript checks both directions of a run-time
// script talking to the host.
func TestScriptCallsGoAndGoCallsScript(t *testing.T) {
	env := lug.New()
	lug.Bind(env, "double", func(n float64) float64 { return n * 2 })
	p := &Player{HP: 50}
	lug.Expose(env, "player", p)

	rets, err := lug.DoString(env, `
		player:Damage(10)
		function answer(n) return double(n) + player.HP end
		return answer(4)
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	if n, _ := lug.As[float64](rets[0]); n != 48 { // 8 + 40
		t.Fatalf("answer(4) = %v, want 48", n)
	}

	// The function the script defined is an ordinary Lua value.
	rets, err = lug.CallErr(env, env.Get("answer"), lug.Number(5))
	if err != nil {
		t.Fatalf("calling answer from Go: %v", err)
	}
	n, _ := lug.As[float64](rets[0])
	if n != 50 { // 10 + 40
		t.Fatalf("answer(5) = %v, want 50", n)
	}
}

// TestLuaLoadAndDofile checks the Lua-level loaders, which only exist because
// importing lug installs the interpreter.
func TestLuaLoadAndDofile(t *testing.T) {
	env := lug.New()

	rets, err := lug.DoString(env, `
		local chunk = load("return 6 * 7", "=inline")
		local a = chunk()
		local bad, msg = load("return +", "=broken")
		return a, bad, msg
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	if n, _ := lug.As[float64](rets[0]); n != 42 {
		t.Fatalf("load(...)() = %v, want 42", n)
	}
	if !rets[1].IsNil() {
		t.Fatalf("load of broken source returned %v, want nil", rets[1])
	}
	if msg, _ := lug.As[string](rets[2]); msg == "" {
		t.Fatal("load of broken source returned an empty error message")
	}

	// dofile from Lua, and DoFile from Go.
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.lua")
	if err := os.WriteFile(path, []byte("return 'from file'"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.Set("path", lug.ValueOf(path))
	rets, err = lug.DoString(env, "return dofile(path)")
	if err != nil {
		t.Fatalf("dofile: %v", err)
	}
	if s, _ := lug.As[string](rets[0]); s != "from file" {
		t.Fatalf("dofile returned %q", s)
	}

	rets, err = lug.DoFile(env, path)
	if err != nil {
		t.Fatalf("DoFile: %v", err)
	}
	if s, _ := lug.As[string](rets[0]); s != "from file" {
		t.Fatalf("DoFile returned %q", s)
	}
}

// TestScriptErrors checks that syntax errors, runtime errors and pcall all
// behave like they do on the transpiled path.
func TestScriptErrors(t *testing.T) {
	env := lug.New()

	if _, err := lug.DoString(env, "return +"); err == nil {
		t.Fatal("a syntax error must be reported")
	}
	if _, err := lug.DoString(env, "local x; return x.field"); err == nil {
		t.Fatal("a runtime error must be reported")
	}

	// pcall inside interpreted code catches interpreted errors.
	rets, err := lug.DoString(env, `
		local ok, msg = pcall(function() error("boom") end)
		return ok, msg
	`)
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	if ok, _ := lug.As[bool](rets[0]); ok {
		t.Fatal("pcall reported success for a failing function")
	}
	if msg, _ := lug.As[string](rets[1]); msg == "" {
		t.Fatal("pcall lost the error message")
	}

	// An error from a Go binding surfaces the same way.
	lug.Bind(env, "explode", func() error { return os.ErrPermission })
	if _, err := lug.DoString(env, "explode()"); err == nil {
		t.Fatal("an error returned by a Go binding must be raised")
	}
}

// TestScriptMutualRecursion mixes both execution modes: a transpiled chunk
// (emulated here by a Go closure) calls an interpreted script and vice versa.
func TestScriptMutualRecursion(t *testing.T) {
	env := lug.New()
	script, err := lug.Compile(`return 1`, "=leaf")
	if err != nil {
		t.Fatal(err)
	}
	leaf := script.Fn()

	// Transpiled-style code: plain Go calling a Lua function value.
	env.Set("leaf", lug.ValueOf(leaf))
	env.Set("go_host", lug.ValueOf(lug.Fn(func(e *lug.Env, args []lug.Value) []lug.Value {
		n := lug.Get[float64](args, 0)
		child, _ := lug.CallErr(e, e.Get("leaf"))
		return []lug.Value{lug.Number(n + lug.Get[float64](child, 0))}
	})))

	rets, err := lug.DoString(env, "return go_host(41)")
	if err != nil {
		t.Fatalf("DoString: %v", err)
	}
	if n, _ := lug.As[float64](rets[0]); n != 42 {
		t.Fatalf("go_host(41) = %v, want 42", n)
	}
}

// TestSandbox checks what a sandboxed environment can and cannot do.
func TestSandbox(t *testing.T) {
	bare := lug.NewSandboxed("bare", lug.SandboxOptions{})
	for _, name := range []string{"os", "io", "load", "loadfile", "dofile", "require", "debug", "print"} {
		if v := bare.Get(name); !v.IsNil() {
			t.Errorf("sandboxed env exposes %s = %v", name, v)
		}
	}
	// The pure library is still there.
	if _, err := lug.DoString(bare, "return string.rep('a', 3), table.concat({1, 2}, '-'), math.floor(1.5)"); err != nil {
		t.Fatalf("pure library missing: %v", err)
	}

	var got []string
	printer := lug.NewSandboxed("tag", lug.SandboxOptions{
		AllowPrint: true,
		OnPrint:    func(tag, line string) { got = append(got, tag+": "+line) },
	})
	if _, err := lug.DoString(printer, "print('a', 1)"); err != nil {
		t.Fatalf("print: %v", err)
	}
	if len(got) != 1 || got[0] != "tag: a\t1" {
		t.Fatalf("print was routed as %q", got)
	}

	// os/io are subsets: no execute, no getenv, no file handles.
	partial := lug.NewSandboxed("part", lug.SandboxOptions{OS: true, IO: true})
	rets, err := lug.DoString(partial, "return type(os.time()), os.execute, os.getenv, io.open, type(io.write)")
	if err != nil {
		t.Fatalf("partial sandbox: %v", err)
	}
	if first, _ := lug.As[string](rets[0]); first != "number" {
		t.Fatalf("os.time returned %v, want a number", rets[0])
	}
	if !rets[1].IsNil() || !rets[2].IsNil() || !rets[3].IsNil() {
		t.Fatalf("dangerous os/io entries leaked: %v %v %v", rets[1], rets[2], rets[3])
	}
	if w, _ := lug.As[string](rets[4]); w != "function" {
		t.Fatalf("io.write is %v, want a function", rets[4])
	}

	// Two sandboxes never share globals.
	a := lug.NewSandboxed("a", lug.SandboxOptions{})
	b := lug.NewSandboxed("b", lug.SandboxOptions{})
	if _, err := lug.DoString(a, "shared = 'a'"); err != nil {
		t.Fatal(err)
	}
	if v := b.Get("shared"); !v.IsNil() {
		t.Fatalf("sandboxes share globals: b.shared = %v", v)
	}
}
