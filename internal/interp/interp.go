// Package interp runs Lua scripts by walking the AST. It is the only execution
// path in lug: a compiled script is an ordinary lg.Fn, so it takes (env, args)
// and returns values, which makes interpreted functions interchangeable with Go
// functions, pcall, coroutines, metatables, Bind and Expose.
//
// Cost, measured on an Apple M3 against gopher-lua's bytecode VM: fib(24) takes
// 32ms here versus 8.5ms, and a 100k numeric loop 4.5ms versus 8.6ms. The tree
// walker dispatches every node through an interface type switch and boxes every
// value in 48 bytes, which is why calls cost more than a VM's register moves;
// in exchange there is nothing to generate, load or cache, and scripts can be
// read from disk, a string or the network while the program runs.
//
// Two things dominate the design because they were measured, not guessed:
// scopes preallocate their bindings (capacity computed at parse time) and are
// reused across loop iterations unless the body contains a function literal,
// and statement-level shapes like `local x = e` and `x = e` skip the temporary
// slices that a uniform "list of expressions" path would allocate.
package interp

package interp

import (
	"sync/atomic"

	"github.com/pulseaiclub/lug/internal/ast"
	"github.com/pulseaiclub/lug/internal/parser"
	lg "github.com/pulseaiclub/lug/runtime"
)

// maxDepth bounds Lua call depth. Without it, runaway recursion would exhaust
// the Go stack, and a Go stack overflow is a fatal error that recover cannot
// catch — it would take the whole process down instead of raising a Lua error.
const maxDepth = 20000

// Program is a parsed script plus its recursion counter. Compile once and run
// many times: parsing is the expensive part.
type Program struct {
	src   string
	file  *ast.File
	depth atomic.Int64
	// top wraps the chunk's statements so they share the block counts below.
	top *ast.Block
	// locals is how many locals each block can hold, which preallocates scopes
	// so declaring a local never allocates or moves the bindings.
	locals map[*ast.Block]int
	// closure marks blocks that contain a function literal. Only those need a
	// fresh scope per loop iteration, because only a closure can observe the
	// difference between one shared binding and one binding per iteration.
	closure map[*ast.Block]bool
}

// Compile prepares a parsed file for interpretation. src is used in error
// messages and as the function name.
func Compile(file *ast.File, src string) *Program {
	p := &Program{src: src, file: file, top: &ast.Block{Stmts: file.Stmts},
		locals: make(map[*ast.Block]int, 8), closure: make(map[*ast.Block]bool, 8)}
	p.locals[p.top] = countLocals(p.top, p.locals)
	for b := range p.locals {
		p.closure[b] = containsFunc(b)
	}
	return p
}

// countLocals records how many locals the statements of b declare in b's own
// scope, and visits every nested block and function body so all of them are
// counted too. Missing one shows up as a scope overflow at run time rather than
// as silent corruption, which is why declare checks the capacity.
func countLocals(b *ast.Block, out map[*ast.Block]int) int {
	n := 0
	walkStmts(b.Stmts, out, func(extra int) { n += extra })
	out[b] = n
	return n
}

// containsFunc reports whether a function literal appears anywhere inside b,
// which is the only way a loop iteration's binding can be observed after the
// iteration ends.
func containsFunc(b *ast.Block) bool {
	found := false
	var walkE func(ast.Expr)
	walkE = func(e ast.Expr) {
		if found || e == nil {
			return
		}
		switch x := e.(type) {
		case *ast.FuncExpr:
			found = true
		case *ast.BinExpr:
			walkE(x.L)
			walkE(x.R)
		case *ast.UnExpr:
			walkE(x.X)
		case *ast.ParenExpr:
			walkE(x.X)
		case *ast.IndexExpr:
			walkE(x.Obj)
			walkE(x.Key)
		case *ast.CallExpr:
			walkE(x.Fn)
			for _, a := range x.Args {
				walkE(a)
			}
		case *ast.MethodExpr:
			walkE(x.Obj)
			for _, a := range x.Args {
				walkE(a)
			}
		case *ast.TableExpr:
			for _, f := range x.Fields {
				walkE(f.Key)
				walkE(f.Val)
			}
		}
	}
	var walkB func(*ast.Block)
	walkB = func(blk *ast.Block) {
		if found || blk == nil {
			return
		}
		for _, st := range blk.Stmts {
			switch x := st.(type) {
			case *ast.LocalFuncStmt, *ast.FuncStmt:
				found = true
			case *ast.LocalStmt:
				for _, v := range x.Vals {
					walkE(v)
				}
			case *ast.AssignStmt:
				for _, t := range x.Targets {
					walkE(t)
				}
				for _, v := range x.Vals {
					walkE(v)
				}
			case *ast.CallStmt:
				walkE(x.Call)
			case *ast.DoStmt:
				walkB(x.Body)
			case *ast.WhileStmt:
				walkE(x.Cond)
				walkB(x.Body)
			case *ast.RepeatStmt:
				walkB(x.Body)
				walkE(x.Cond)
			case *ast.IfStmt:
				for i, c := range x.Conds {
					walkE(c)
					walkB(x.Blocks[i])
				}
				walkB(x.Else)
			case *ast.NumForStmt:
				walkE(x.Init)
				walkE(x.Limit)
				walkE(x.Step)
				walkB(x.Body)
			case *ast.GenForStmt:
				for _, e := range x.Exprs {
					walkE(e)
				}
				walkB(x.Body)
			case *ast.ReturnStmt:
				for _, v := range x.Vals {
					walkE(v)
				}
			}
		}
	}
	walkB(b)
	return found
}

// walkStmts walks statements, reporting how many locals each declares in the
// enclosing scope and recursing into nested blocks and function bodies.
func walkStmts(stmts []ast.Stmt, out map[*ast.Block]int, add func(int)) {
	for _, st := range stmts {
		switch x := st.(type) {
		case *ast.LocalStmt:
			add(len(x.Names))
			for _, v := range x.Vals {
				walkExpr(v, out)
			}
		case *ast.LocalFuncStmt:
			add(1)
			countLocals(x.Body, out)
		case *ast.FuncStmt:
			walkExpr(x.Target, out)
			countLocals(x.Body, out)
		case *ast.AssignStmt:
			for _, t := range x.Targets {
				walkExpr(t, out)
			}
			for _, v := range x.Vals {
				walkExpr(v, out)
			}
		case *ast.CallStmt:
			walkExpr(x.Call, out)
		case *ast.DoStmt:
			countLocals(x.Body, out)
		case *ast.WhileStmt:
			walkExpr(x.Cond, out)
			countLocals(x.Body, out)
		case *ast.RepeatStmt:
			countLocals(x.Body, out)
			walkExpr(x.Cond, out)
		case *ast.IfStmt:
			for i, c := range x.Conds {
				walkExpr(c, out)
				countLocals(x.Blocks[i], out)
			}
			if x.Else != nil {
				countLocals(x.Else, out)
			}
		case *ast.NumForStmt:
			add(1) // the control variable gets a scope per iteration
			for _, e := range []ast.Expr{x.Init, x.Limit, x.Step} {
				if e != nil {
					walkExpr(e, out)
				}
			}
			countLocals(x.Body, out)
		case *ast.GenForStmt:
			add(len(x.Names))
			for _, e := range x.Exprs {
				walkExpr(e, out)
			}
			countLocals(x.Body, out)
		case *ast.ReturnStmt:
			for _, v := range x.Vals {
				walkExpr(v, out)
			}
		}
	}
}

// walkExpr visits the function literals inside an expression.
func walkExpr(e ast.Expr, out map[*ast.Block]int) {
	switch x := e.(type) {
	case nil:
		return
	case *ast.FuncExpr:
		countLocals(x.Body, out)
	case *ast.BinExpr:
		walkExpr(x.L, out)
		walkExpr(x.R, out)
	case *ast.UnExpr:
		walkExpr(x.X, out)
	case *ast.ParenExpr:
		walkExpr(x.X, out)
	case *ast.IndexExpr:
		walkExpr(x.Obj, out)
		walkExpr(x.Key, out)
	case *ast.CallExpr:
		walkExpr(x.Fn, out)
		for _, a := range x.Args {
			walkExpr(a, out)
		}
	case *ast.MethodExpr:
		walkExpr(x.Obj, out)
		for _, a := range x.Args {
			walkExpr(a, out)
		}
	case *ast.TableExpr:
		for _, f := range x.Fields {
			walkExpr(f.Key, out)
			walkExpr(f.Val, out)
		}
	}
}

// Parse parses Lua source and prepares it for interpretation.
func Parse(source string, name string) (*Program, error) {
	file, err := parser.Parse([]byte(source), name)
	if err != nil {
		return nil, err
	}
	return Compile(file, name), nil
}

// Source returns the chunk name the program was compiled from.
func (p *Program) Source() string { return p.src }

// Chunk returns the script as a lg.Fn. The returned function can be stored,
// passed to Go code, called from Lua, or run concurrently; each call gets a
// fresh top-level scope.
func (p *Program) Chunk() lg.Fn {
	return func(env *lg.Env, args []lg.Value) []lg.Value {
		n := p.depth.Add(1)
		defer p.depth.Add(-1)
		if n > maxDepth {
			lg.Errorf("%s: stack overflow (interpreted call depth exceeds %d)", p.src, maxDepth)
		}
		f := &frame{prog: p, env: env, args: args, scope: newScope(nil, p.locals[p.top])}
		f.block(p.top)
		return f.rets
	}
}

// Run executes the script in env and returns its results. Lua errors are
// returned as an error instead of panicking.
func (p *Program) Run(env *lg.Env, args ...lg.Value) (rets []lg.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = lg.WrapPanic(r)
		}
	}()
	return p.Chunk()(env, args), nil
}

// ---------------------------------------------------------------------------
// scopes and control flow
// ---------------------------------------------------------------------------

// scope holds the locals of one block. A linear slice beats a map here: Lua
// blocks hold a handful of names, and looking up "k" in a two-entry slice is
// far cheaper than hashing it.
//
// Values live inline in the slice, not behind individual pointers, so declaring
// a local costs no allocation; a closure can still see later updates because it
// captures the scope and lookup returns a pointer into the slice. That is only
// sound while the slice never grows, which is why the capacity is computed by
// countLocals at parse time and why declare reports an overflow instead of
// reallocating.
type scope struct {
	parent *scope
	vars   []binding
}

type binding struct {
	name string
	val  lg.Value
}

func newScope(parent *scope, capacity int) *scope {
	return &scope{parent: parent, vars: make([]binding, 0, capacity)}
}

func (s *scope) declare(name string, v lg.Value) *binding {
	if len(s.vars) == cap(s.vars) {
		// Growing would invalidate the *binding handed to closures, so this is
		// a bug in the capacity analysis rather than something to recover from.
		lg.Errorf("interp: internal error: scope holds more than %d locals", cap(s.vars))
	}
	s.vars = append(s.vars, binding{name: name, val: v})
	return &s.vars[len(s.vars)-1]
}

func (s *scope) lookup(name string) *binding {
	for sc := s; sc != nil; sc = sc.parent {
		// Later bindings shadow earlier ones in the same block.
		for i := len(sc.vars) - 1; i >= 0; i-- {
			if sc.vars[i].name == name {
				return &sc.vars[i]
			}
		}
	}
	return nil
}

// flow is how a statement stopped. Break, return and goto unwind the block
// stack; everything else is flowNormal.
type flow int

const (
	flowNormal flow = iota
	flowBreak
	flowReturn
	flowGoto
)

// frame is one function activation.
type frame struct {
	prog    *Program
	env     *lg.Env
	scope   *scope
	args    []lg.Value // varargs (arguments beyond the named parameters)
	rets    []lg.Value // set by return
	gotoLbl string     // pending goto target
}

func (f *frame) block(b *ast.Block) flow {
	var depths map[int]int // binding count at each label, once it has run
	for pc := 0; pc < len(b.Stmts); pc++ {
		if _, ok := b.Stmts[pc].(*ast.LabelStmt); ok {
			if depths == nil {
				depths = make(map[int]int, 2)
			}
			depths[pc] = len(f.scope.vars)
		}
		fl := f.stmt(b.Stmts[pc])
		if fl == flowGoto {
			// A goto may target a label in this block or in an enclosing one;
			// enclosing blocks handle it when this block propagates up.
			if idx, ok := labelIndex(b, f.gotoLbl); ok {
				// Jumping backwards leaves the locals declared after the label,
				// so drop them; the label re-declares whatever it needs.
				if d, seen := depths[idx]; seen && idx <= pc {
					f.scope.vars = f.scope.vars[:d]
				}
				pc = idx
				continue
			}
			return flowGoto
		}
		if fl != flowNormal {
			return fl
		}
	}
	return flowNormal
}

func labelIndex(b *ast.Block, name string) (int, bool) {
	for i, st := range b.Stmts {
		if l, ok := st.(*ast.LabelStmt); ok && l.Name == name {
			return i, true
		}
	}
	return 0, false
}

func (f *frame) stmt(s ast.Stmt) flow {
	switch x := s.(type) {
	case *ast.LocalStmt:
		// The single-name case dominates real code and needs no slice.
		if len(x.Names) == 1 {
			v := lg.Nil
			if len(x.Vals) == 1 {
				v = f.eval(x.Vals[0])
			}
			f.scope.declare(x.Names[0], v)
			return flowNormal
		}
		vals := f.evalList(x.Vals, len(x.Names))
		for i, name := range x.Names {
			f.scope.declare(name, lg.Arg(vals, i))
		}
	case *ast.LocalFuncStmt:
		b := f.scope.declare(x.Name, lg.Nil) // declare first: the body may recurse
		b.val = f.funcValue(x.Name, x.Params, x.Vararg, x.Body)
	case *ast.FuncStmt:
		fn := f.funcValue(x.Method, x.Params, x.Vararg, x.Body)
		if x.Method == "" {
			f.assign(x.Target, fn)
		} else {
			obj := f.eval(x.Target)
			lg.SetIndex(f.env, obj, lg.String(x.Method), fn)
		}
	case *ast.AssignStmt:
		f.assignList(x.Targets, x.Vals)
	case *ast.CallStmt:
		f.discard(x.Call)
	case *ast.DoStmt:
		return f.inScope(x.Body)
	case *ast.WhileStmt:
		for lg.Truth(f.eval(x.Cond)) {
			fl := f.inScope(x.Body)
			if fl == flowBreak {
				return flowNormal
			}
			if fl != flowNormal {
				return fl
			}
		}
	case *ast.RepeatStmt:
		outer := f.scope
		for {
			// The condition sees the body's locals, so it is evaluated while
			// that scope is still installed.
			f.scope = newScope(outer, f.prog.locals[x.Body])
			fl := f.block(x.Body)
			if fl == flowBreak {
				f.scope = outer
				return flowNormal
			}
			if fl != flowNormal {
				f.scope = outer
				return fl
			}
			done := lg.Truth(f.eval(x.Cond))
			f.scope = outer
			if done {
				return flowNormal
			}
		}
	case *ast.IfStmt:
		for i, cond := range x.Conds {
			if lg.Truth(f.eval(cond)) {
				return f.inScope(x.Blocks[i])
			}
		}
		if x.Else != nil {
			return f.inScope(x.Else)
		}
	case *ast.NumForStmt:
		return f.numFor(x)
	case *ast.GenForStmt:
		return f.genFor(x)
	case *ast.ReturnStmt:
		f.rets = f.evalList(x.Vals, -1)
		return flowReturn
	case *ast.BreakStmt:
		return flowBreak
	case *ast.GotoStmt:
		f.gotoLbl = x.Label
		return flowGoto
	case *ast.LabelStmt:
		// Labels are resolved by block(); nothing to do at run time.
	}
	return flowNormal
}

func (f *frame) inScope(b *ast.Block) flow {
	old := f.scope
	f.scope = newScope(old, f.prog.locals[b])
	fl := f.block(b)
	f.scope = old
	return fl
}

func (f *frame) numFor(x *ast.NumForStmt) flow {
	initV, ok := lg.ToNumber(f.eval(x.Init))
	if !ok {
		lg.Errorf("bad 'for' initial value (number expected, got %s)", typeOf(f.eval(x.Init)))
	}
	limitV, ok := lg.ToNumber(f.eval(x.Limit))
	if !ok {
		lg.Errorf("bad 'for' limit (number expected, got %s)", typeOf(f.eval(x.Limit)))
	}
	step := 1.0
	if x.Step != nil {
		s, ok := lg.ToNumber(f.eval(x.Step))
		if !ok {
			lg.Errorf("bad 'for' step (number expected, got %s)", typeOf(f.eval(x.Step)))
		}
		step = s
	}
	if step == 0 {
		lg.ErrorString("'for' step is zero")
	}
	outer := f.scope
	defer func() { f.scope = outer }()
	// The body shares its scope with the control variable, so both must fit.
	reused := newScope(outer, 1+f.prog.locals[x.Body])
	fresh := f.prog.closure[x.Body]
	if !fresh {
		reused.declare(x.Var, lg.Nil)
	}
	for v := initV; (step > 0 && v <= limitV) || (step < 0 && v >= limitV); v += step {
		if fresh {
			// A closure in the body may capture the control variable, so each
			// iteration needs its own binding.
			f.scope = newScope(outer, 1+f.prog.locals[x.Body])
			f.scope.declare(x.Var, lg.Number(v))
		} else {
			// Otherwise one binding is enough and the body's locals are dropped
			// before the next iteration reuses the slots.
			reused.vars = reused.vars[:1]
			reused.vars[0].val = lg.Number(v)
			f.scope = reused
		}
		fl := f.block(x.Body)
		if fl == flowBreak {
			return flowNormal
		}
		if fl != flowNormal {
			return fl
		}
	}
	return flowNormal
}

func (f *frame) genFor(x *ast.GenForStmt) flow {
	vals := f.evalList(x.Exprs, 3)
	fn, state, ctrl := lg.Arg(vals, 0), lg.Arg(vals, 1), lg.Arg(vals, 2)
	for {
		rets := lg.Call(f.env, fn, []lg.Value{state, ctrl})
		ctrl = lg.Arg(rets, 0)
		if ctrl.IsNil() {
			return flowNormal
		}
		old := f.scope
		f.scope = newScope(old, len(x.Names)+f.prog.locals[x.Body])
		for i, name := range x.Names {
			f.scope.declare(name, lg.Arg(rets, i))
		}
		fl := f.block(x.Body)
		f.scope = old
		_ = fl
		if fl == flowBreak {
			return flowNormal
		}
		if fl != flowNormal {
			return fl
		}
	}
}

// assignList evaluates every target prefix and key before any value is stored,
// which is what Lua guarantees (`a[i], a[j] = a[j], a[i]` swaps).
func (f *frame) assignList(targets []ast.Expr, vals []ast.Expr) {
	// The common case: one target, which needs no slot bookkeeping.
	if len(targets) == 1 {
		switch t := targets[0].(type) {
		case *ast.NameExpr:
			v := lg.Nil
			if len(vals) == 1 {
				v = f.eval(vals[0])
			} else if len(vals) > 1 {
				v = lg.Arg(f.evalList(vals, -1), 0)
			}
			if b := f.scope.lookup(t.Name); b != nil {
				b.val = v
				return
			}
			f.env.Set(t.Name, v)
			return
		case *ast.IndexExpr:
			obj := f.eval(t.Obj)
			key := f.eval(t.Key)
			v := lg.Nil
			if len(vals) == 1 {
				v = f.eval(vals[0])
			} else if len(vals) > 1 {
				v = lg.Arg(f.evalList(vals, -1), 0)
			}
			lg.SetIndex(f.env, obj, key, v)
			return
		}
	}
	type slot struct {
		b   *binding
		obj lg.Value
		key lg.Value
		idx bool
	}
	slots := make([]slot, len(targets))
	for i, t := range targets {
		switch x := t.(type) {
		case *ast.NameExpr:
			if b := f.scope.lookup(x.Name); b != nil {
				slots[i] = slot{b: b}
			} else {
				slots[i] = slot{obj: f.env.Globals(), key: lg.String(x.Name), idx: true}
			}
		case *ast.IndexExpr:
			slots[i] = slot{obj: f.eval(x.Obj), key: f.eval(x.Key), idx: true}
		default:
			lg.Errorf("cannot assign to this expression")
		}
	}
	values := f.evalList(vals, len(targets))
	for i, sl := range slots {
		if sl.idx {
			lg.SetIndex(f.env, sl.obj, sl.key, lg.Arg(values, i))
		} else {
			sl.b.val = lg.Arg(values, i)
		}
	}
}

// assign stores one value into a name or index target.
func (f *frame) assign(target ast.Expr, v lg.Value) {
	switch x := target.(type) {
	case *ast.NameExpr:
		if b := f.scope.lookup(x.Name); b != nil {
			b.val = v
			return
		}
		f.env.Set(x.Name, v)
	case *ast.IndexExpr:
		lg.SetIndex(f.env, f.eval(x.Obj), f.eval(x.Key), v)
	default:
		lg.Errorf("cannot assign to this expression")
	}
}

// ---------------------------------------------------------------------------
// expressions
// ---------------------------------------------------------------------------

// eval evaluates an expression to exactly one value (truncating multiple
// results), which is what most contexts need.
func (f *frame) eval(e ast.Expr) lg.Value {
	switch x := e.(type) {
	case *ast.NilExpr:
		return lg.Nil
	case *ast.BoolExpr:
		return lg.Bool(x.Val)
	case *ast.NumberExpr:
		return lg.Number(x.Val)
	case *ast.StringExpr:
		return lg.String(x.Val)
	case *ast.VarargExpr:
		return lg.Arg(f.args, 0)
	case *ast.NameExpr:
		if b := f.scope.lookup(x.Name); b != nil {
			return b.val
		}
		return f.env.Get(x.Name)
	case *ast.ParenExpr:
		return f.eval(x.X)
	case *ast.IndexExpr:
		return lg.Index(f.env, f.eval(x.Obj), f.eval(x.Key))
	case *ast.CallExpr:
		return lg.Arg(f.callExpr(x), 0)
	case *ast.MethodExpr:
		return lg.Arg(f.methodExpr(x), 0)
	case *ast.FuncExpr:
		return f.funcValue("", x.Params, x.Vararg, x.Body)
	case *ast.TableExpr:
		return f.tableExpr(x)
	case *ast.UnExpr:
		return f.unExpr(x)
	case *ast.BinExpr:
		return f.binExpr(x)
	}
	lg.Errorf("unsupported expression %T", e)
	return lg.Nil
}

// evalMulti evaluates an expression that may produce several values.
func (f *frame) evalMulti(e ast.Expr) []lg.Value {
	switch x := e.(type) {
	case *ast.CallExpr:
		return f.callExpr(x)
	case *ast.MethodExpr:
		return f.methodExpr(x)
	case *ast.VarargExpr:
		return f.args
	}
	return []lg.Value{f.eval(e)}
}

// evalList evaluates an expression list. want >= 0 adjusts the result to that
// many values (padding with nil); want < 0 keeps everything the last
// expression produced, which is what argument and return lists need.
func (f *frame) evalList(es []ast.Expr, want int) []lg.Value {
	out := make([]lg.Value, 0, len(es))
	for i, e := range es {
		if i == len(es)-1 {
			out = append(out, f.evalMulti(e)...)
			break
		}
		out = append(out, f.eval(e))
	}
	if want >= 0 {
		for len(out) < want {
			out = append(out, lg.Nil)
		}
		if len(out) > want {
			out = out[:want]
		}
	}
	return out
}

func (f *frame) callExpr(x *ast.CallExpr) []lg.Value {
	fn := f.eval(x.Fn)
	return lg.Call(f.env, fn, f.evalList(x.Args, -1))
}

func (f *frame) methodExpr(x *ast.MethodExpr) []lg.Value {
	obj := f.eval(x.Obj)
	fn := lg.Index(f.env, obj, lg.String(x.Name))
	args := make([]lg.Value, 0, len(x.Args)+1)
	args = append(args, obj)
	args = append(args, f.evalList(x.Args, -1)...)
	return lg.Call(f.env, fn, args)
}

func (f *frame) tableExpr(x *ast.TableExpr) lg.Value {
	t := lg.NewTable()
	n := 0
	for i, fd := range x.Fields {
		last := i == len(x.Fields)-1
		if fd.Key == nil {
			var vals []lg.Value
			if last {
				vals = f.evalMulti(fd.Val)
			} else {
				vals = []lg.Value{f.eval(fd.Val)}
			}
			for _, v := range vals {
				n++
				t.Set(lg.Number(float64(n)), v)
			}
			continue
		}
		k := f.eval(fd.Key)
		var v lg.Value
		if last {
			v = lg.Arg(f.evalMulti(fd.Val), 0)
		} else {
			v = f.eval(fd.Val)
		}
		t.Set(k, v)
	}
	return lg.TableVal(t)
}

func (f *frame) unExpr(x *ast.UnExpr) lg.Value {
	switch x.Op {
	case ast.UnNeg:
		return lg.Unm(f.env, f.eval(x.X))
	case ast.UnNot:
		return lg.Bool(!lg.Truth(f.eval(x.X)))
	case ast.UnLen:
		return lg.Len(f.env, f.eval(x.X))
	case ast.UnBnot:
		return lg.BitNot(f.env, f.eval(x.X))
	}
	lg.Errorf("unsupported unary operator")
	return lg.Nil
}

func (f *frame) binExpr(x *ast.BinExpr) lg.Value {
	// and/or return one of their operands and must not evaluate the right
	// side when the left side decides.
	switch x.Op {
	case ast.OpAnd:
		l := f.eval(x.L)
		if !lg.Truth(l) {
			return l
		}
		return f.eval(x.R)
	case ast.OpOr:
		l := f.eval(x.L)
		if lg.Truth(l) {
			return l
		}
		return f.eval(x.R)
	}
	a := f.eval(x.L)
	b := f.eval(x.R)
	switch x.Op {
	case ast.OpAdd:
		return lg.Add(f.env, a, b)
	case ast.OpSub:
		return lg.Sub(f.env, a, b)
	case ast.OpMul:
		return lg.Mul(f.env, a, b)
	case ast.OpDiv:
		return lg.Div(f.env, a, b)
	case ast.OpMod:
		return lg.Mod(f.env, a, b)
	case ast.OpPow:
		return lg.Pow(f.env, a, b)
	case ast.OpIDiv:
		return lg.IDiv(f.env, a, b)
	case ast.OpConcat:
		return lg.Concat(f.env, a, b)
	case ast.OpEQ:
		return lg.Bool(lg.EQ(f.env, a, b))
	case ast.OpNE:
		return lg.Bool(!lg.EQ(f.env, a, b))
	case ast.OpLT:
		return lg.Bool(lg.LT(f.env, a, b))
	case ast.OpLE:
		return lg.Bool(lg.LE(f.env, a, b))
	case ast.OpGT:
		return lg.Bool(lg.LT(f.env, b, a))
	case ast.OpGE:
		return lg.Bool(lg.LE(f.env, b, a))
	case ast.OpBor:
		return lg.BitOr(f.env, a, b)
	case ast.OpBxor:
		return lg.BitXor(f.env, a, b)
	case ast.OpBand:
		return lg.BitAnd(f.env, a, b)
	case ast.OpShl:
		return lg.Shl(f.env, a, b)
	case ast.OpShr:
		return lg.Shr(f.env, a, b)
	}
	lg.Errorf("unsupported binary operator")
	return lg.Nil
}

// funcValue builds a closure over the current scope.
func (f *frame) funcValue(name string, params []string, vararg bool, body *ast.Block) lg.Value {
	parent := f.scope
	prog := f.prog
	defEnv := f.env
	fn := lg.NewClosure(func(env *lg.Env, args []lg.Value) []lg.Value {
		if env == nil {
			env = defEnv
		}
		n := prog.depth.Add(1)
		defer prog.depth.Add(-1)
		if n > maxDepth {
			lg.Errorf("stack overflow (interpreted call depth exceeds %d)", maxDepth)
		}
		nf := frame{prog: prog, env: env, args: args}
		nf.scope = newScope(parent, len(params)+prog.locals[body])
		for i, p := range params {
			nf.scope.declare(p, lg.Arg(args, i))
		}
		if vararg {
			nf.args = lg.Tail(args, len(params))
		}
		if fl := nf.block(body); fl == flowReturn {
			return nf.rets
		}
		return nil
	}, closureName(name))
	return fn
}

func closureName(name string) string {
	if name == "" {
		return "?"
	}
	return name
}

// discard evaluates an expression statement for its side effects, keeping all
// of a call's results out of the way.
func (f *frame) discard(e ast.Expr) {
	switch x := e.(type) {
	case *ast.CallExpr:
		f.callExpr(x)
	case *ast.MethodExpr:
		f.methodExpr(x)
	default:
		f.eval(e)
	}
}

func typeOf(v lg.Value) string { return v.Type() }
