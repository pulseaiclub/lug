package runtime

import "sync"

// Coroutines with Lua semantics, one goroutine per coroutine.
//
// Channel protocol. Each Coroutine owns two unbuffered channels:
//
//	resumeCh  resumer -> body   first resume: the body's arguments;
//	                        later resumes: the results of the pending yield
//	yieldCh   body -> resumer  what happened: yielded (yield's arguments),
//	                        returned (the body's results) or failed (*LuaError)
//
// resume marks the coroutine running, hands it the arguments on resumeCh and
// blocks on yieldCh. The body goroutine runs the function; coroutine.yield
// (running on the body goroutine) sends a "yielded" event on yieldCh and then
// blocks on resumeCh for the next resume. Because both channels are
// unbuffered, exactly one of the two goroutines is running Lua code at any
// moment; that is what keeps a single Env safe without any further locking.
//
// Current coroutine. yield, isyieldable and running must know which
// coroutine the caller is executing in, but Fn only carries the *Env. The
// running coroutine is therefore recorded per Env (see setCurrentCoroutine).
// resume records it before handing control to the body and restores the
// previous entry after the body hands control back, so a body always observes
// itself as current and, once it yields, its resumer observes the caller it
// had. resume must be called with the same Env the coroutine was created in
// (interpreted code always is).
//
// Goroutine leak. A coroutine that is abandoned while suspended keeps its
// body goroutine blocked on resumeCh forever: it leaks one goroutine. Lua's
// collector would free it; Go cannot preempt a channel-blocked goroutine, so
// the leak is accepted. A coroutine that is never resumed starts no goroutine
// (the body is launched lazily by the first resume).

// Coroutine is a Lua coroutine (type "thread"): a function that can suspend
// itself with coroutine.yield and be resumed with coroutine.resume. The body
// runs on its own goroutine, started by the first resume.
type Coroutine struct {
	mu      sync.Mutex // guards state and started
	state   coState
	started bool // body goroutine has been launched

	fn  Fn   // the body as given to create
	env *Env // the Env captured by create; the body runs against it

	resumeCh chan []Value // resumer -> body
	yieldCh  chan coEvent // body -> resumer
}

// coState is the internal status of a Coroutine; the names are the strings
// coroutine.status reports.
type coState uint8

const (
	coSuspended coState = iota
	coRunning
	coNormal // it resumed another coroutine, which is now running
	coDead
)

func (s coState) String() string {
	switch s {
	case coSuspended:
		return "suspended"
	case coRunning:
		return "running"
	case coNormal:
		return "normal"
	}
	return "dead"
}

// coEvent is the body's report to the resumer.
type coEvent struct {
	kind coEventKind
	vals []Value   // coYielded: yield's arguments; coReturned: the body's results
	err  *LuaError // coFailed: the error the body raised
}

type coEventKind uint8

const (
	coYielded coEventKind = iota
	coReturned
	coFailed
)

// Thread returns the coroutine, or nil if v is not a thread.
func (v Value) Thread() *Coroutine {
	if v.kind == KindThread {
		return v.ptr.(*Coroutine)
	}
	return nil
}

func (co *Coroutine) value() Value { return Value{kind: KindThread, ptr: co} }

// Status returns "suspended", "running", "normal" or "dead": the value
// coroutine.status reports for co.
func (co *Coroutine) Status() string {
	co.mu.Lock()
	defer co.mu.Unlock()
	return co.state.String()
}

func (co *Coroutine) setState(s coState) {
	co.mu.Lock()
	co.state = s
	co.mu.Unlock()
}

// The coroutine whose body is currently running, per Env. Guarded by
// coCurMu; the resume/yield handshake guarantees that an entry is only ever
// observed by the one goroutine allowed to run Lua code in that Env.
var (
	coCurMu   sync.Mutex
	coCurrent = make(map[*Env]*Coroutine)
)

func currentCoroutine(env *Env) *Coroutine {
	coCurMu.Lock()
	co := coCurrent[env]
	coCurMu.Unlock()
	return co
}

// setCurrentCoroutine makes co the current coroutine of env (nil clears it)
// and returns the coroutine that was current before.
func setCurrentCoroutine(env *Env, co *Coroutine) *Coroutine {
	coCurMu.Lock()
	prev := coCurrent[env]
	if co == nil {
		delete(coCurrent, env)
	} else {
		coCurrent[env] = co
	}
	coCurMu.Unlock()
	return prev
}

func init() { installers = append(installers, installCoroutine) }

// installCoroutine registers the global "coroutine" table.
func installCoroutine(e *Env) {
	lib := NewTable()
	set := func(name string, fn Fn) { lib.Set(String(name), NewClosure(fn, name)) }
	set("create", coroutineCreate)
	set("resume", coroutineResume)
	set("yield", coroutineYield)
	set("status", coroutineStatus)
	set("wrap", coroutineWrap)
	set("isyieldable", coroutineIsYieldable)
	set("running", coroutineRunning)
	e.Set("coroutine", TableVal(lib))
}

// newCoroutine builds a suspended coroutine for f. from names the library
// function called, for the argument error message.
func newCoroutine(env *Env, f Value, from string) *Coroutine {
	body := f.Closure()
	if body == nil {
		// Tables and userdata call through __call (see Call); nothing else is
		// a function.
		m := MetaOf(f)
		if m == nil || m.Get(String("__call")).kind != KindFunction {
			Errorf("bad argument #1 to '%s' (function expected, got %s)", from, f.Type())
		}
	}
	co := &Coroutine{
		env:      env,
		resumeCh: make(chan []Value),
		yieldCh:  make(chan coEvent),
	}
	if body != nil {
		co.fn = body.Fn
	} else {
		co.fn = func(env *Env, args []Value) []Value { return Call(env, f, args) }
	}
	return co
}

func coroutineCreate(env *Env, args []Value) []Value {
	return []Value{newCoroutine(env, Arg(args, 0), "create").value()}
}

func coroutineResume(env *Env, args []Value) []Value {
	co := Arg(args, 0).Thread()
	if co == nil {
		Errorf("bad argument #1 to 'resume' (coroutine expected)")
	}
	return co.resume(env, Tail(args, 1))
}

// resume runs co's body until it yields or returns. It returns
// {true, results...} when the body yields or finishes, and {false, err} when
// the body raised an error or co cannot be resumed (dead, or not suspended).
func (co *Coroutine) resume(env *Env, args []Value) []Value {
	// Status check and transition happen under the mutex; the channel
	// handshake happens outside it so that resuming a coroutine that is not
	// suspended is refused instead of deadlocking.
	co.mu.Lock()
	if co.state != coSuspended {
		msg := "cannot resume non-suspended coroutine"
		if co.state == coDead {
			msg = "cannot resume dead coroutine"
		}
		co.mu.Unlock()
		return []Value{False, String(msg)}
	}
	co.state = coRunning
	first := !co.started
	co.started = true
	co.mu.Unlock()

	// The coroutine we are running in (nil on the main coroutine) is "normal"
	// while it waits for us and "running" again once we hand control back.
	prev := setCurrentCoroutine(env, co)
	if prev != nil {
		prev.setState(coNormal)
	}
	if first {
		go co.body()
	}
	co.resumeCh <- args // the body's first arguments, or a pending yield's results
	ev := <-co.yieldCh  // yielded, returned or failed

	setCurrentCoroutine(env, prev)
	if prev != nil {
		prev.setState(coRunning)
	}
	if ev.kind == coYielded {
		co.setState(coSuspended)
	} else {
		co.setState(coDead)
	}

	if ev.kind == coFailed {
		return []Value{False, ev.err.Value}
	}
	return append([]Value{True}, ev.vals...)
}

// body is the coroutine's goroutine. It runs the function once and reports
// the outcome on yieldCh; Lua errors and Go panics are captured here and
// never propagate on this goroutine.
func (co *Coroutine) body() {
	args := <-co.resumeCh
	var results []Value
	err := Protect(func() { results = co.fn(co.env, args) })
	if err != nil {
		co.yieldCh <- coEvent{kind: coFailed, err: err}
		return
	}
	co.yieldCh <- coEvent{kind: coReturned, vals: results}
}

// coroutineYield suspends the running coroutine, handing vals to its
// resumer, and returns the argument list of the next resume.
func coroutineYield(env *Env, args []Value) []Value {
	co := currentCoroutine(env)
	if co == nil {
		ErrorString("attempt to yield from outside a coroutine")
	}
	co.yieldCh <- coEvent{kind: coYielded, vals: args}
	return <-co.resumeCh
}

func coroutineStatus(env *Env, args []Value) []Value {
	co := Arg(args, 0).Thread()
	if co == nil {
		Errorf("bad argument #1 to 'status' (coroutine expected)")
	}
	return []Value{String(co.Status())}
}

// coroutineWrap returns a function that resumes the coroutine and returns its
// results. Errors, including "cannot resume dead coroutine", are raised
// rather than returned.
func coroutineWrap(env *Env, args []Value) []Value {
	co := newCoroutine(env, Arg(args, 0), "wrap")
	return []Value{NewClosure(func(env *Env, args []Value) []Value {
		r := co.resume(env, args)
		if !r[0].Truth() {
			Error(r[1])
		}
		return r[1:]
	}, "wrap")}
}

func coroutineIsYieldable(env *Env, args []Value) []Value {
	return []Value{Bool(currentCoroutine(env) != nil)}
}

// coroutineRunning returns the running thread (nil for the main coroutine)
// and whether it is the main coroutine.
func coroutineRunning(env *Env, args []Value) []Value {
	if co := currentCoroutine(env); co != nil {
		return []Value{co.value(), False}
	}
	return []Value{Nil, True}
}
