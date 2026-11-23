// Package interp runs Lua scripts at run time by walking the AST.
//
// The transpiler in internal/codegen turns Lua into Go at build time, which is
// the fast path. This package covers the other half: a host program that wants
// to load a script from disk or a string while it runs. Interpreted code and
// transpiled code are interchangeable, because a compiled script is an
// ordinary lg.Fn: it takes (env, args) and returns values, so pcall,
// coroutines, metatables, Bind and Expose all apply to both.
//
// The cost is speed. Measured on this machine: fib(24) takes 34ms interpreted
// versus 0.14ms transpiled, a 100k numeric loop 14ms versus 0.08ms. Two orders
// of magnitude, because the tree walker dispatches every node through an
// interface type switch, boxes every value in 48 bytes, and allocates a scope
// plus an argument slice per call — none of which the Go compiler can undo.
// For reference, gopher-lua's bytecode VM needs 8.5ms for the same fib(24), so
// this interpreter is ~4x slower than a dedicated VM and ~250x slower than the
// transpiler. Use it for the scripting layer, keep hot paths transpiled.
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
}

// Compile prepares a parsed file for interpretation. src is used in error
// messages and as the function name.
func Compile(file *ast.File, src string) *Program {
	return &Program{src: src, file: file}
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
		f := &frame{prog: p, env: env, args: args, scope: newScope(nil)}
		f.block(&ast.Block{Stmts: p.file.Stmts})
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

// scope holds locals. A linear slice beats a map here: Lua blocks hold a
// handful of names, and looking up "k" in a 2-entry slice is far cheaper than
// hashing it. Values live behind pointers so a closure keeps seeing updates
// made after it was created, exactly like Lua upvalues.
type scope struct {
	parent *scope
	vars   []binding
}

type binding struct {
	name string
	cell *lg.Value
}

func newScope(parent *scope) *scope { return &scope{parent: parent} }

func (s *scope) declare(name string, v lg.Value) *lg.Value {
	cell := new(lg.Value)
	*cell = v
	s.vars = append(s.vars, binding{name: name, cell: cell})
	return cell
}

func (s *scope) lookup(name string) *lg.Value {
	for sc := s; sc != nil; sc = sc.parent {
		// Later bindings shadow earlier ones in the same block.
		for i := len(sc.vars) - 1; i >= 0; i-- {
			if sc.vars[i].name == name {
				return sc.vars[i].cell
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
	for pc := 0; pc < len(b.Stmts); pc++ {
		fl := f.stmt(b.Stmts[pc])
		if fl == flowGoto {
			// A goto may target a label in this block or in an enclosing one;
			// enclosing blocks handle it when this block propagates up.
			if idx, ok := labelIndex(b, f.gotoLbl); ok {
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
		vals := f.evalList(x.Vals, len(x.Names))
		for i, name := range x.Names {
			f.scope.declare(name, lg.Arg(vals, i))
		}
	case *ast.LocalFuncStmt:
		cell := f.scope.declare(x.Name, lg.Nil) // declare first: the body may recurse
		*cell = f.funcValue(x.Name, x.Params, x.Vararg, x.Body)
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
			f.scope = newScope(outer)
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
	f.scope = newScope(old)
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
	for v := initV; (step > 0 && v <= limitV) || (step < 0 && v >= limitV); v += step {
		f.scope = newScope(outer)
		f.scope.declare(x.Var, lg.Number(v)) // fresh cell per iteration
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
		f.scope = newScope(old)
		for i, name := range x.Names {
			f.scope.declare(name, lg.Arg(rets, i))
		}
		fl := f.block(x.Body)
		f.scope = old
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
	type slot struct {
		cell *lg.Value
		obj  lg.Value
		key  lg.Value
		idx  bool
	}
	slots := make([]slot, len(targets))
	for i, t := range targets {
		switch x := t.(type) {
		case *ast.NameExpr:
			if cell := f.scope.lookup(x.Name); cell != nil {
				slots[i] = slot{cell: cell}
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
			*sl.cell = lg.Arg(values, i)
		}
	}
}

// assign stores one value into a name or index target.
func (f *frame) assign(target ast.Expr, v lg.Value) {
	switch x := target.(type) {
	case *ast.NameExpr:
		if cell := f.scope.lookup(x.Name); cell != nil {
			*cell = v
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
		if cell := f.scope.lookup(x.Name); cell != nil {
			return *cell
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
		nf := frame{prog: prog, env: env, scope: newScope(parent)}
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
