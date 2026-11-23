package runtime

// Fn is the signature of every Lua callable: interpreted code, library
// functions and host Go functions all use it.
// Functions receive the environment and the argument list and return their
// results as a slice (nil/empty means no values).
type Fn func(env *Env, args []Value) []Value

// Closure is a callable Lua value.
type Closure struct {
	Fn   Fn
	Name string // for debug messages
}

// NewClosure wraps fn into a callable Value.
func NewClosure(fn Fn, name string) Value {
	return Value{kind: KindFunction, ptr: &Closure{Fn: fn, Name: name}}
}

// Call invokes f with args. Panics with *LuaError when f is not callable.
// Tables and userdata with a __call metamethod are called through it.
func Call(env *Env, f Value, args []Value) []Value {
	switch f.kind {
	case KindFunction:
		return f.ptr.(*Closure).Fn(env, args)
	case KindTable, KindUserdata:
		if m := MetaOf(f); m != nil {
			if h := m.Get(String("__call")); h.kind == KindFunction {
				return h.ptr.(*Closure).Fn(env, append([]Value{f}, args...))
			}
		}
	}
	Errorf("attempt to call a %s value", f.Type())
	return nil
}

// Call1 invokes f and returns only its first result (nil when absent).
// This is what every single-value expression context compiles to.
func Call1(env *Env, f Value, args []Value) Value {
	r := Call(env, f, args)
	if len(r) == 0 {
		return Nil
	}
	return r[0]
}

// Arg returns args[i] or nil when out of range. Generated code binds each
// declared parameter through this helper, which makes missing arguments nil.
func Arg(args []Value, i int) Value {
	if i >= 0 && i < len(args) {
		return args[i]
	}
	return Nil
}

// Tail returns args[i:], the vararg tail of a function's parameters.
func Tail(args []Value, i int) []Value {
	if i >= len(args) {
		return nil
	}
	return args[i:]
}

// Pick returns vals[i] or nil, padding short multiple-value lists.
func Pick(vals []Value, i int) Value {
	if i >= 0 && i < len(vals) {
		return vals[i]
	}
	return Nil
}

// ExtendArray appends values to t starting at integer index start
// (used for table constructors whose last field is a multi-value call).
func ExtendArray(t *Table, vals []Value, start int) {
	for i, v := range vals {
		t.Set(Number(float64(start+i)), v)
	}
}

// MetaOf returns the metatable attached to a table or userdata, else nil.
func MetaOf(v Value) *Table {
	switch v.kind {
	case KindTable:
		return v.ptr.(*Table).meta
	case KindUserdata:
		return v.ptr.(*Userdata).meta
	}
	return nil
}

// ToStringEnv converts a value to string, honoring the __tostring
// metamethod (used by tostring() and print()).
func ToStringEnv(env *Env, v Value) string {
	if m := MetaOf(v); m != nil {
		if h := m.Get(String("__tostring")); h.kind == KindFunction {
			r := Call1(env, h, []Value{v})
			if r.kind == KindString {
				return r.str
			}
			ErrorString("'__tostring' must return a string")
		}
	}
	return ToString(v)
}
