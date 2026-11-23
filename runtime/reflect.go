package runtime

import (
	"math"
	"reflect"
)

// This file implements the Go <-> Lua bridge used by lug.Bind/lug.Expose:
//
//   - WrapFunc wraps an arbitrary Go function into a Lua closure. All
//     reflection happens once, at bind time; each call converts arguments
//     through the cached reflect.Type.
//   - Userdata holding a Go value exposes exported struct fields and
//     methods to Lua through userdataIndex/userdataSetIndex. Method wrappers
//     are cached per userdata value.
//
// Pass pointers to structs so both value- and pointer-receiver methods are
// visible to Lua.

var (
	errType   = reflect.TypeOf((*error)(nil)).Elem()
	valueType = reflect.TypeOf(Value{})
)

// Pack converts a Go value to a Lua value, mapping the types Go and Lua
// share — numbers, strings, booleans, tables, functions — onto their Lua
// kinds, and wrapping anything else as userdata (like WrapValue). Host code
// should prefer Pack over WrapValue when building values from Go data.
func Pack(v any) Value {
	switch x := v.(type) {
	case nil:
		return Nil
	case Value:
		return x
	case float64:
		return Number(x)
	case float32:
		return Number(float64(x))
	case int:
		return Number(float64(x))
	case int64:
		return Number(float64(x))
	case int32:
		return Number(float64(x))
	case int16:
		return Number(float64(x))
	case int8:
		return Number(float64(x))
	case uint:
		return Number(float64(x))
	case uint64:
		return Number(float64(x))
	case uint32:
		return Number(float64(x))
	case uint16:
		return Number(float64(x))
	case uint8:
		return Number(float64(x))
	case string:
		return String(x)
	case bool:
		return Bool(x)
	case *Table:
		return TableVal(x)
	}
	return WrapValue(v)
}

// WrapValue converts a Go value to a Lua value: Value passes through,
// *Table/Fn/Closure become their Lua kinds, functions become closures,
// everything else becomes userdata.
func WrapValue(v any) Value {
	switch x := v.(type) {
	case nil:
		return Nil
	case Value:
		return x
	case *Table:
		return TableVal(x)
	case Fn:
		return NewClosure(x, "go")
	case *Closure:
		return Value{kind: KindFunction, ptr: x}
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Func {
		return wrapFunc(rv, "go")
	}
	return UserdataVal(NewUserdata(v))
}

// WrapFunc wraps a Go function into a Lua closure via reflection.
//
// Accepted parameter types: Value, float64, float32, int, int64, string,
// bool, *Table, slices and arrays of those (converted from a table's array
// part), maps (converted from a table's key/value pairs), interface{} (unwraps
// userdata), plus a variadic tail.
// Return values become Lua values; a final error return raises a Lua error.
func WrapFunc(fn any) Value {
	rv := reflect.ValueOf(fn)
	if rv.Kind() != reflect.Func {
		ErrorString("lug.WrapFunc: target must be a function")
	}
	return wrapFunc(rv, "go")
}

func wrapFunc(rv reflect.Value, name string) Value {
	rt := rv.Type()
	return NewClosure(func(env *Env, args []Value) []Value {
		return callReflect(env, rt, func(in []reflect.Value) []reflect.Value {
			return rv.Call(in)
		}, args)
	}, name)
}

// callReflect converts Lua args to reflect args, calls call, and converts
// the results back.
func callReflect(env *Env, rt reflect.Type, call func([]reflect.Value) []reflect.Value, args []Value) []Value {
	nin := rt.NumIn()
	in := make([]reflect.Value, 0, nin)
	for i := 0; i < nin; i++ {
		if rt.IsVariadic() && i == nin-1 {
			et := rt.In(i).Elem()
			for j := i; j < len(args); j++ {
				in = append(in, toGo(Arg(args, j), et, j))
			}
			break
		}
		in = append(in, toGo(Arg(args, i), rt.In(i), i))
	}
	out := call(in)
	res := make([]Value, 0, len(out))
	for i := range out {
		if rt.Out(i) == errType {
			if !out[i].IsNil() {
				Error(String(out[i].Interface().(error).Error()))
			}
			continue
		}
		res = append(res, fromGo(out[i]))
	}
	return res
}

func toGo(v Value, t reflect.Type, argIdx int) reflect.Value {
	if t == valueType {
		return reflect.ValueOf(v)
	}
	switch t.Kind() {
	case reflect.Float64, reflect.Float32:
		if f, ok := ToNumber(v); ok {
			return reflect.ValueOf(f).Convert(t)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if f, ok := ToNumber(v); ok && f == math.Trunc(f) {
			return reflect.ValueOf(int64(f)).Convert(t)
		}
	case reflect.String:
		switch v.kind {
		case KindString:
			return reflect.ValueOf(v.str)
		case KindNumber:
			return reflect.ValueOf(NumToStr(v.num))
		}
	case reflect.Bool:
		if v.kind == KindBool {
			return reflect.ValueOf(v.BoolVal())
		}
	case reflect.Interface:
		if t.NumMethod() == 0 { // any: unwrap
			return reflect.ValueOf(goAny(v))
		}
	case reflect.Slice, reflect.Array:
		if v.kind == KindTable {
			tbl := v.ptr.(*Table)
			n := tbl.Len()
			var out reflect.Value
			if t.Kind() == reflect.Array {
				if n != t.Len() {
					Errorf("bad argument #%d (%s wants %d items, got %d)", argIdx+1, t.String(), t.Len(), n)
				}
				out = reflect.New(t).Elem()
			} else {
				out = reflect.MakeSlice(t, n, n)
			}
			for i := 0; i < n; i++ {
				out.Index(i).Set(toGo(tbl.Get(Number(float64(i+1))), t.Elem(), argIdx))
			}
			return out
		}
	case reflect.Map:
		if v.kind == KindTable {
			tbl := v.ptr.(*Table)
			out := reflect.MakeMap(t)
			for k, val, ok := tbl.Next(Nil); ok; k, val, ok = tbl.Next(k) {
				out.SetMapIndex(toGo(k, t.Key(), argIdx), toGo(val, t.Elem(), argIdx))
			}
			return out
		}
	case reflect.Ptr, reflect.Struct:
		if v.kind == KindUserdata {
			dv := reflect.ValueOf(v.ptr.(*Userdata).Data)
			if dv.IsValid() && dv.Type().AssignableTo(t) {
				return dv
			}
		}
		if v.kind == KindTable && t == reflect.TypeOf((*Table)(nil)) {
			return reflect.ValueOf(v.ptr.(*Table))
		}
	}
	Errorf("bad argument #%d (%s expected, got %s)", argIdx+1, t.String(), v.Type())
	return reflect.Value{}
}

func goAny(v Value) any {
	switch v.kind {
	case KindUserdata:
		return v.ptr.(*Userdata).Data
	case KindTable:
		return v.ptr.(*Table)
	}
	return v
}

func fromGo(rv reflect.Value) Value {
	if !rv.IsValid() {
		return Nil
	}
	switch rv.Kind() {
	case reflect.Bool:
		return Bool(rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return Number(float64(rv.Int()))
	case reflect.Float32, reflect.Float64:
		return Number(rv.Float())
	case reflect.String:
		return String(rv.String())
	case reflect.Ptr:
		if t, ok := rv.Interface().(*Table); ok {
			return TableVal(t)
		}
		if rv.IsNil() {
			return Nil
		}
	}
	if rv.Type() == valueType {
		return rv.Interface().(Value)
	}
	return UserdataVal(NewUserdata(rv.Interface()))
}

// userdataIndex resolves a field or method access on wrapped Go data.
// It is the fallback when the userdata has no usable __index metamethod.
func userdataIndex(env *Env, u *Userdata, key Value) (Value, bool) {
	if key.kind != KindString {
		return Nil, false
	}
	name := key.str
	if v, ok := u.cache[name]; ok {
		return v, true
	}
	rv := reflect.ValueOf(u.Data)
	if !rv.IsValid() {
		return Nil, false
	}
	// Struct field (through a pointer too).
	sv := rv
	if sv.Kind() == reflect.Ptr && !sv.IsNil() {
		sv = sv.Elem()
	}
	if sv.Kind() == reflect.Struct {
		if f := sv.FieldByName(name); f.IsValid() && f.CanInterface() {
			return fromGo(f), true
		}
	}
	// Method (value or pointer receiver).
	if m := rv.MethodByName(name); m.IsValid() {
		mv := wrapMethod(u, m, name)
		if u.cache == nil {
			u.cache = make(map[string]Value)
		}
		u.cache[name] = mv
		return mv, true
	}
	return Nil, false
}

func wrapMethod(u *Userdata, m reflect.Value, name string) Value {
	rt := m.Type()
	return NewClosure(func(env *Env, args []Value) []Value {
		// Lua's `obj:m(a)` passes the receiver as the first argument, but a
		// bound method value already has it bound: drop it when it is this
		// userdata, so `obj:m(a)` and `obj.m(a)` both work.
		if len(args) > 0 && args[0].Userdata() == u {
			args = args[1:]
		}
		return callReflect(env, rt, func(in []reflect.Value) []reflect.Value {
			return m.Call(in)
		}, args)
	}, name)
}

// userdataSetIndex assigns to a settable struct field on wrapped Go data.
func userdataSetIndex(env *Env, u *Userdata, key, val Value) bool {
	if key.kind != KindString {
		return false
	}
	rv := reflect.ValueOf(u.Data)
	if rv.Kind() == reflect.Ptr && !rv.IsNil() {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return false
	}
	f := rv.FieldByName(key.str)
	if !f.IsValid() || !f.CanSet() {
		return false
	}
	f.Set(toGo(val, f.Type(), 1))
	return true
}

// userdataLen reports the length of wrapped Go slices/maps/strings for #v.
func userdataLen(u *Userdata) Value {
	rv := reflect.ValueOf(u.Data)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map:
		return Int(int64(rv.Len()))
	case reflect.String:
		return Int(int64(rv.Len()))
	}
	return Nil
}
