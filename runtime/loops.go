package runtime

// NumFor drives a numeric for loop (`for i = init, limit, step`).
//
// All values are float64 (single number type), so the loop is a float loop
// like Lua's: it terminates when i passes the limit in the step direction.
// The loop variable is produced fresh for every iteration so closures
// capture distinct values, matching Lua semantics.
type NumFor struct {
	i, limit, step float64
}

// NewNumFor validates and initializes a numeric loop.
func NewNumFor(init, limit, step Value) *NumFor {
	i, ok1 := toNum(init)
	l, ok2 := toNum(limit)
	s, ok3 := toNum(step)
	if !ok1 {
		ErrorString("'for' initial value must be a number")
	}
	if !ok2 {
		ErrorString("'for' limit must be a number")
	}
	if !ok3 {
		ErrorString("'for' step must be a number")
	}
	if s == 0 {
		ErrorString("'for' step is zero")
	}
	return &NumFor{i: i, limit: l, step: s}
}

// Next advances the loop and returns the current value, or ok=false when
// the loop is finished.
// NextNum is Next for loops whose control variable is statically known to
// be a number (the common case in transpiled numeric for loops).
func (n *NumFor) NextNum() (float64, bool) {
	if n.step > 0 {
		if !(n.i <= n.limit) {
			return 0, false
		}
	} else {
		if !(n.i >= n.limit) {
			return 0, false
		}
	}
	v := n.i
	n.i += n.step
	return v, true
}

func (n *NumFor) Next() (Value, bool) {
	if n.step > 0 {
		if !(n.i <= n.limit) {
			return Nil, false
		}
	} else {
		if !(n.i >= n.limit) {
			return Nil, false
		}
	}
	v := Number(n.i)
	n.i += n.step
	return v, true
}

// GenFor drives a generic for loop (`for k, v in f, s, c`): it calls
// f(s, c) each iteration and stops when the first result is nil.
type GenFor struct {
	env       *Env
	f, s, ctl Value
}

// NewGenFor initializes a generic loop from its three control values.
func NewGenFor(env *Env, f, s, ctl Value) *GenFor {
	return &GenFor{env: env, f: f, s: s, ctl: ctl}
}

// Next runs one iteration of the iterator protocol. It returns nil when
// the loop is exhausted, else the iterator's result list. The first result
// becomes the control value passed to the next call (Lua's generic-for
// protocol).
func (g *GenFor) Next() []Value {
	r := Call(g.env, g.f, []Value{g.s, g.ctl})
	if len(r) == 0 || r[0].IsNil() {
		return nil
	}
	g.ctl = r[0]
	return r
}
