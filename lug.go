// Package lug embeds Lua into Go by transpiling Lua source to Go code at
// build time and running it on a small runtime.
//
// Workflow:
//
//	lug gen -pkg game -o scripts_lug.go script.lua   # generates Go source
//
//	// game/scripts_lug.go (generated)
//	var Chunk_script lg.Fn = func(env *lg.Env, args []lg.Value) []lg.Value { ... }
//
//	// host code
//	env := lug.New()
//	lug.Bind(env, "damage", func(p *Player, dmg float64) float64 { ... })
//	rets, err := lug.Run(env, Chunk_script, lug.String("arg"))
//
// The transpiled code has no interpreter overhead: every Lua function is a
// real Go function, locals are Go variables, and the hot helpers in the
// runtime package never use reflection.
package lug

import (
	"github.com/pulseaiclub/lug/runtime"
)

// Value is a Lua value.
type Value = runtime.Value

// Env is a Lua environment (globals + standard library).
type Env = runtime.Env

// Fn is the signature of every Lua callable.
type Fn = runtime.Fn

// Table is a Lua table.
type Table = runtime.Table

// Re-exported constructors and sentinels.
var (
	Nil   = runtime.Nil
	True  = runtime.True
	False = runtime.False
)

// Number returns a number value.
func Number(f float64) Value { return runtime.Number(f) }

// Int returns a number value from an int.
func Int(i int64) Value { return runtime.Int(i) }

// String returns a string value.
func String(s string) Value { return runtime.String(s) }

// Bool returns a boolean value.
func Bool(b bool) Value { return runtime.Bool(b) }

// TableVal wraps a table into a value.
func TableVal(t *Table) Value { return runtime.TableVal(t) }

// ValueOf converts a Go value to a Lua value: numbers, strings, booleans,
// tables and functions map onto their Lua kinds, everything else becomes
// userdata (fields and methods stay accessible from Lua).
func ValueOf(v any) Value { return runtime.Pack(v) }

// NewTable creates an empty table.
func NewTable() *Table { return runtime.NewTable() }

// New creates an environment with the full standard library installed.
func New() *Env { return runtime.New() }

// Bind exposes a Go function to Lua under a global name. The function is
// wrapped once at bind time; the call path is reflection-free afterwards
// only for Value-typed signatures — Go-typed signatures keep a small
// per-call conversion cost (see runtime.WrapFunc for accepted types).
func Bind(env *Env, name string, fn any) { env.Set(name, runtime.WrapFunc(fn)) }

// Expose wraps a Go value (struct, pointer, slice, map) as userdata under a
// global name. Exported struct fields and methods become accessible from
// Lua. Pass pointers so pointer-receiver methods are visible.
func Expose(env *Env, name string, v any) { env.Set(name, runtime.WrapValue(v)) }

// Call invokes a Lua value from Go, raising a *runtime.LuaError on failure.
func Call(env *Env, f Value, args ...Value) []Value { return runtime.Call(env, f, args) }

// Run executes a transpiled chunk, converting Lua errors (panics) into a
// returned error. This is the main entry point for host programs.
func Run(env *Env, chunk Fn, args ...Value) (rets []Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = runtime.WrapPanic(r)
		}
	}()
	return chunk(env, args), nil
}

// MustRun is Run that panics on error.
func MustRun(env *Env, chunk Fn, args ...Value) []Value {
	rets, err := Run(env, chunk, args...)
	if err != nil {
		panic(err)
	}
	return rets
}

// Conversions for reading results.

// AsFloat returns the value as a float64.
func AsFloat(v Value) (float64, bool) { return runtime.ToNumber(v) }

// AsInt returns the value as an int64 when it is integral.
func AsInt(v Value) (int64, bool) {
	f, ok := runtime.ToNumber(v)
	if !ok || f != float64(int64(f)) {
		return 0, false
	}
	return int64(f), true
}

// AsString returns the value's string payload.
func AsString(v Value) (string, bool) {
	if v.IsString() {
		return v.Str(), true
	}
	return "", false
}

// AsBool returns the value's boolean payload.
func AsBool(v Value) (bool, bool) {
	if v.Kind() == runtime.KindBool {
		return v.BoolVal(), true
	}
	return false, false
}

// AsTable returns the value's table.
func AsTable(v Value) (*Table, bool) {
	t := v.Table()
	return t, t != nil
}
