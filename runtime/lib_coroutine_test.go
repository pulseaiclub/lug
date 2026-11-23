package runtime

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// newTestEnv creates an Env with the full standard library. Other library
// files register installers concurrently with this one; if New panics
// because one of them is half-written, wait and retry before giving up.
func newTestEnv(t *testing.T) *Env {
	t.Helper()
	for attempt := 1; ; attempt++ {
		var e *Env
		if err := Protect(func() { e = New() }); err == nil {
			return e
		}
		if attempt >= 5 {
			t.Fatal("runtime.New() kept failing after 5 attempts")
		}
		t.Logf("runtime.New() failed (attempt %d); retrying in 30s", attempt)
		time.Sleep(30 * time.Second)
	}
}

// coroLib returns the coroutine library function `name` as a Value.
func coroLib(t *testing.T, e *Env, name string) Value {
	t.Helper()
	f := Index(e, e.Get("coroutine"), String(name))
	if f.Closure() == nil {
		t.Fatalf("coroutine.%s is missing or not a function (got %s)", name, ToString(f))
	}
	return f
}

func valStr(v Value) string {
	if v.IsString() {
		return strconv.Quote(v.Str())
	}
	return ToString(v)
}

func valsStr(vals []Value) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = valStr(v)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// checkVals asserts got equals want, value by value (RawEqual).
func checkVals(t *testing.T, what string, got []Value, want ...Value) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d values %s, want %d values %s", what, len(got), valsStr(got), len(want), valsStr(want))
	}
	for i := range want {
		if !RawEqual(got[i], want[i]) {
			t.Errorf("%s: value %d: got %s, want %s", what, i+1, valStr(got[i]), valStr(want[i]))
		}
	}
}

// expectError runs f and asserts it raises a Lua error whose value equals want.
func expectError(t *testing.T, what string, want Value, f func()) {
	t.Helper()
	err := Protect(f)
	if err == nil {
		t.Fatalf("%s: expected a Lua error, got none", what)
	}
	if !RawEqual(err.Value, want) {
		t.Fatalf("%s: got error %s, want %s", what, valStr(err.Value), valStr(want))
	}
}

func TestCoroutineBasic(t *testing.T) {
	e := newTestEnv(t)
	create, resume, yield := coroLib(t, e, "create"), coroLib(t, e, "resume"), coroLib(t, e, "yield")

	body := NewClosure(func(env *Env, args []Value) []Value {
		Call(env, yield, []Value{Number(1)})
		Call(env, yield, []Value{Number(2)})
		return []Value{String("done")}
	}, "test")
	co := Call1(e, create, []Value{body})

	if co.Type() != "thread" || co.Thread() == nil {
		t.Fatalf("create returned %s, want a thread", valStr(co))
	}
	if Number(1).Thread() != nil {
		t.Fatal("Thread() on a number should be nil")
	}

	checkVals(t, "resume #1", Call(e, resume, []Value{co}), True, Number(1))
	checkVals(t, "resume #2", Call(e, resume, []Value{co}), True, Number(2))
	checkVals(t, "resume #3", Call(e, resume, []Value{co}), True, String("done"))
	checkVals(t, "resume #4 (dead)", Call(e, resume, []Value{co}), False, String("cannot resume dead coroutine"))
}

func TestCoroutineStatus(t *testing.T) {
	e := newTestEnv(t)
	create, resume, status, yield := coroLib(t, e, "create"), coroLib(t, e, "resume"), coroLib(t, e, "status"), coroLib(t, e, "yield")

	var co Value
	body := NewClosure(func(env *Env, args []Value) []Value {
		// Observed from inside: the coroutine is running.
		Call(env, yield, Call(env, status, []Value{co}))
		return nil
	}, "test")
	co = Call1(e, create, []Value{body})

	checkVals(t, "status after create", Call(e, status, []Value{co}), String("suspended"))
	checkVals(t, "status seen from inside", Call(e, resume, []Value{co}), True, String("running"))
	checkVals(t, "status after yield", Call(e, status, []Value{co}), String("suspended"))
	checkVals(t, "resume to completion", Call(e, resume, []Value{co}), True)
	checkVals(t, "status after completion", Call(e, status, []Value{co}), String("dead"))

	expectError(t, "status(number)", String("bad argument #1 to 'status' (coroutine expected)"), func() {
		Call(e, status, []Value{Number(1)})
	})
}

func TestCoroutineNormal(t *testing.T) {
	e := newTestEnv(t)
	create, resume, status := coroLib(t, e, "create"), coroLib(t, e, "resume"), coroLib(t, e, "status")

	var a, b Value
	bBody := NewClosure(func(env *Env, args []Value) []Value {
		resumeA := Call(env, resume, []Value{a}) // our resumer is "normal", not suspended
		return []Value{
			Call1(env, status, []Value{a}), // "normal": a resumed us and is waiting
			Call1(env, status, []Value{b}), // "running": that is us
			resumeA[0], resumeA[1],         // resuming our resumer is refused
		}
	}, "b")
	aBody := NewClosure(func(env *Env, args []Value) []Value {
		resumed := Call(env, resume, []Value{b})
		return append([]Value{Call1(env, status, []Value{a})}, resumed...)
	}, "a")
	a = Call1(e, create, []Value{aBody})
	b = Call1(e, create, []Value{bBody})

	checkVals(t, "resume a", Call(e, resume, []Value{a}),
		True,                                                   // a yields/returns normally to main
		String("running"),                                      // status(a) seen from a
		True,                                                   // a resumed b
		String("normal"),                                       // status(a) seen from b
		String("running"),                                      // status(b) seen from b
		False, String("cannot resume non-suspended coroutine")) // b tries to resume a
	checkVals(t, "status a afterwards", Call(e, status, []Value{a}), String("dead"))
	checkVals(t, "status b afterwards", Call(e, status, []Value{b}), String("dead"))
}

func TestCoroutineSelfResume(t *testing.T) {
	e := newTestEnv(t)
	create, resume, status := coroLib(t, e, "create"), coroLib(t, e, "resume"), coroLib(t, e, "status")

	var co Value
	body := NewClosure(func(env *Env, args []Value) []Value {
		return Call(env, resume, []Value{co}) // resume myself while running
	}, "test")
	co = Call1(e, create, []Value{body})

	// The inner resume is refused with false+message; the body returns that
	// pair, so the outer resume reports success plus the body's results.
	checkVals(t, "self resume", Call(e, resume, []Value{co}), True, False, String("cannot resume non-suspended coroutine"))
	checkVals(t, "status afterwards", Call(e, status, []Value{co}), String("dead"))
}

func TestCoroutineError(t *testing.T) {
	e := newTestEnv(t)
	create, resume, yield := coroLib(t, e, "create"), coroLib(t, e, "resume"), coroLib(t, e, "yield")

	cases := []struct {
		name string
		body Fn
		want Value
	}{
		{"lua string error", func(env *Env, args []Value) []Value {
			Error(String("boom"))
			return nil
		}, String("boom")},
		{"lua non-string error", func(env *Env, args []Value) []Value {
			Error(Number(42))
			return nil
		}, Number(42)},
		{"error after yield", func(env *Env, args []Value) []Value {
			Call(env, yield, []Value{Number(1)})
			ErrorString("late boom")
			return nil
		}, String("late boom")},
		{"go panic", func(env *Env, args []Value) []Value {
			panic("go boom")
		}, String("go boom")},
	}
	for _, tc := range cases {
		co := Call1(e, create, []Value{NewClosure(tc.body, tc.name)})
		if tc.name == "error after yield" {
			checkVals(t, tc.name+" (first resume)", Call(e, resume, []Value{co}), True, Number(1))
		}
		checkVals(t, tc.name, Call(e, resume, []Value{co}), False, tc.want)
		checkVals(t, tc.name+" (resume after error)", Call(e, resume, []Value{co}), False, String("cannot resume dead coroutine"))
	}
}

func TestCoroutineWrap(t *testing.T) {
	e := newTestEnv(t)
	wrap, yield := coroLib(t, e, "wrap"), coroLib(t, e, "yield")

	gen := NewClosure(func(env *Env, args []Value) []Value {
		x := Call(env, yield, []Value{Number(1), String("two")})
		y := Call(env, yield, []Value{x[0], String("mid")})
		return append(y, args...) // args: the first call's arguments
	}, "gen")
	g := Call1(e, wrap, []Value{gen})
	if g.Closure() == nil {
		t.Fatalf("wrap returned %s, want a function", valStr(g))
	}
	checkVals(t, "g('a')", Call(e, g, []Value{String("a")}), Number(1), String("two"))
	checkVals(t, "g('b')", Call(e, g, []Value{String("b")}), String("b"), String("mid"))
	checkVals(t, "g('c', 'd')", Call(e, g, []Value{String("c"), String("d")}), String("c"), String("d"), String("a"))
	expectError(t, "g() on a dead coroutine", String("cannot resume dead coroutine"), func() {
		Call(e, g, nil)
	})

	boom := NewClosure(func(env *Env, args []Value) []Value {
		Call(env, yield, []Value{Number(7)})
		Error(String("wrapped boom"))
		return nil
	}, "boom")
	h := Call1(e, wrap, []Value{boom})
	checkVals(t, "h() first call", Call(e, h, nil), Number(7))
	expectError(t, "h() propagates the body's error", String("wrapped boom"), func() {
		Call(e, h, nil)
	})
}

func TestCoroutineYieldOutside(t *testing.T) {
	e := newTestEnv(t)
	yield := coroLib(t, e, "yield")
	expectError(t, "yield from the main coroutine", String("attempt to yield from outside a coroutine"), func() {
		Call(e, yield, []Value{Number(1)})
	})
}

func TestCoroutineYieldPassesResumeArgs(t *testing.T) {
	e := newTestEnv(t)
	create, resume, yield := coroLib(t, e, "create"), coroLib(t, e, "resume"), coroLib(t, e, "yield")

	body := NewClosure(func(env *Env, args []Value) []Value {
		a := Call(env, yield, []Value{Number(1)})
		b := Call(env, yield, []Value{Number(2)})
		out := append([]Value{}, args...)
		out = append(out, a...)
		return append(out, b...)
	}, "test")
	co := Call1(e, create, []Value{body})

	checkVals(t, "resume(init) starts the body", Call(e, resume, []Value{co, String("init")}), True, Number(1))
	checkVals(t, "resume(x) feeds the first yield", Call(e, resume, []Value{co, String("x")}), True, Number(2))
	checkVals(t, "resume(y, z) feeds the second yield", Call(e, resume, []Value{co, String("y"), String("z")}),
		True, String("init"), String("x"), String("y"), String("z"))
	checkVals(t, "resume(dead)", Call(e, resume, []Value{co}), False, String("cannot resume dead coroutine"))
}

func TestCoroutineRunning(t *testing.T) {
	e := newTestEnv(t)
	create, resume := coroLib(t, e, "create"), coroLib(t, e, "resume")
	running, isyieldable := coroLib(t, e, "running"), coroLib(t, e, "isyieldable")

	checkVals(t, "running() on the main coroutine", Call(e, running, nil), Nil, True)
	checkVals(t, "isyieldable() on the main coroutine", Call(e, isyieldable, nil), False)

	var co Value
	body := NewClosure(func(env *Env, args []Value) []Value {
		return append(Call(env, running, nil), Call(env, isyieldable, nil)...)
	}, "test")
	co = Call1(e, create, []Value{body})
	checkVals(t, "inside the coroutine", Call(e, resume, []Value{co}), True, co, False, True)
}

func TestCoroutineAcrossEnvs(t *testing.T) {
	e1, e2 := newTestEnv(t), newTestEnv(t)

	// Each body yields its own thread, as reported by running() in its Env.
	mk := func(e *Env) Value {
		create, yield, running := coroLib(t, e, "create"), coroLib(t, e, "yield"), coroLib(t, e, "running")
		return Call1(e, create, []Value{NewClosure(func(env *Env, args []Value) []Value {
			Call(env, yield, []Value{Call1(env, running, nil)})
			return []Value{String("done")}
		}, "test")})
	}
	co1, co2 := mk(e1), mk(e2)
	r1, r2 := coroLib(t, e1, "resume"), coroLib(t, e2, "resume")

	checkVals(t, "e1 resume #1", Call(e1, r1, []Value{co1}), True, co1)
	checkVals(t, "e2 resume #1", Call(e2, r2, []Value{co2}), True, co2)
	checkVals(t, "e1 resume #2", Call(e1, r1, []Value{co1}), True, String("done"))
	checkVals(t, "e2 resume #2", Call(e2, r2, []Value{co2}), True, String("done"))
}

func TestCoroutineCallableTable(t *testing.T) {
	e := newTestEnv(t)
	create, resume, yield := coroLib(t, e, "create"), coroLib(t, e, "resume"), coroLib(t, e, "yield")

	tbl := NewTable()
	meta := NewTable()
	meta.Set(String("__call"), NewClosure(func(env *Env, args []Value) []Value {
		Call(env, yield, []Value{Arg(args, 0)}) // the table itself, as __call passes it
		return []Value{Arg(args, 1)}
	}, "__call"))
	tbl.SetMetatable(meta)

	co := Call1(e, create, []Value{TableVal(tbl)})
	checkVals(t, "resume(9)", Call(e, resume, []Value{co, Number(9)}), True, TableVal(tbl))
	checkVals(t, "resume()", Call(e, resume, []Value{co}), True, Number(9))
}

func TestCoroutineBadArgs(t *testing.T) {
	e := newTestEnv(t)
	create, resume, wrap := coroLib(t, e, "create"), coroLib(t, e, "resume"), coroLib(t, e, "wrap")

	expectError(t, "create(nil)", String("bad argument #1 to 'create' (function expected, got nil)"), func() {
		Call(e, create, []Value{Nil})
	})
	expectError(t, "create(number)", String("bad argument #1 to 'create' (function expected, got number)"), func() {
		Call(e, create, []Value{Number(1)})
	})
	expectError(t, "create(table without __call)", String("bad argument #1 to 'create' (function expected, got table)"), func() {
		Call(e, create, []Value{TableVal(NewTable())})
	})
	expectError(t, "wrap(table without __call)", String("bad argument #1 to 'wrap' (function expected, got table)"), func() {
		Call(e, wrap, []Value{TableVal(NewTable())})
	})
	expectError(t, "resume(nil)", String("bad argument #1 to 'resume' (coroutine expected)"), func() {
		Call(e, resume, []Value{Nil})
	})
	expectError(t, "resume()", String("bad argument #1 to 'resume' (coroutine expected)"), func() {
		Call(e, resume, nil)
	})
}
