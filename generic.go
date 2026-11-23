package lug

import (
	"fmt"

	"github.com/pulseaiclub/lug/runtime"
)

// Generic helpers for reading results. The old AsFloat/AsInt/... functions
// remain for callers that prefer explicit names; these let the destination
// type be written once, at the call site.
//
//	v, ok := lug.As[float64](rets[0])
//	hp := lug.Get[float64](rets, 0)
//	n, err := lug.Run1[float64](env, Chunk_score, lug.String("easy"))

// As converts v to T. Supported types are Value, float64, int64, int,
// string, bool and *Table; any other type reports false.
func As[T any](v Value) (T, bool) {
	var zero T
	switch any(zero).(type) {
	case Value:
		return any(v).(T), true
	case float64:
		if f, ok := runtime.ToNumber(v); ok {
			return any(f).(T), true
		}
	case int64:
		if i, ok := AsInt(v); ok {
			return any(i).(T), true
		}
	case int:
		if i, ok := AsInt(v); ok {
			return any(int(i)).(T), true
		}
	case string:
		if s, ok := AsString(v); ok {
			return any(s).(T), true
		}
	case bool:
		if b, ok := AsBool(v); ok {
			return any(b).(T), true
		}
	case *Table:
		if t, ok := AsTable(v); ok {
			return any(t).(T), true
		}
	}
	return zero, false
}

// Get returns rets[i] as T. It panics when the index is out of range or the
// value has another type: both are programming errors in host code, not Lua
// errors. Use GetOK when the shape of the result is not known ahead of time.
func Get[T any](rets []Value, i int) T {
	v, ok := GetOK[T](rets, i)
	if !ok {
		if i < 0 || i >= len(rets) {
			panic(fmt.Sprintf("lug: result index %d out of range (chunk returned %d values)", i, len(rets)))
		}
		panic(fmt.Sprintf("lug: result %d is %s, not %T", i, rets[i].Type(), *new(T)))
	}
	return v
}

// GetOK returns rets[i] as T, reporting false when the index is out of range
// or the value has another type.
func GetOK[T any](rets []Value, i int) (T, bool) {
	var zero T
	if i < 0 || i >= len(rets) {
		return zero, false
	}
	return As[T](rets[i])
}

// Run1 runs a chunk that returns at least one value and converts the first
// one to T. Lua errors become a Go error; so does a type mismatch.
func Run1[T any](env *Env, chunk Fn, args ...Value) (T, error) {
	var zero T
	rets, err := Run(env, chunk, args...)
	if err != nil {
		return zero, err
	}
	if len(rets) == 0 {
		return zero, fmt.Errorf("lug: chunk returned no values, want %T", zero)
	}
	v, ok := As[T](rets[0])
	if !ok {
		return zero, fmt.Errorf("lug: chunk returned %s, want %T", rets[0].Type(), zero)
	}
	return v, nil
}

// Slice converts the dense array part of a Lua table (keys 1..n) into a Go
// slice. It reports an error when an element does not have type T, which is
// usually a sign that the Lua side and the Go side disagree.
func Slice[T any](t *Table) ([]T, error) {
	if t == nil {
		return nil, fmt.Errorf("lug: nil table")
	}
	n := t.Len()
	out := make([]T, n)
	for i := 0; i < n; i++ {
		v := t.Get(Int(int64(i + 1)))
		x, ok := As[T](v)
		if !ok {
			return nil, fmt.Errorf("lug: table element %d is %s, want %T", i+1, v.Type(), *new(T))
		}
		out[i] = x
	}
	return out, nil
}

// TableOf builds a Lua array from a Go slice, wrapping each element with the
// same rules as Expose.
func TableOf[T any](items []T) *Table {
	t := NewTable()
	for i, it := range items {
		t.Set(Int(int64(i+1)), runtime.Pack(any(it)))
	}
	return t
}

// Userdata extracts the Go value exposed with Expose. T is the type passed to
// Expose, so for a struct exposed as *Player both *Player and Player work
// (the latter returns a pointer to a copy).
func Userdata[T any](v Value) (*T, bool) {
	u := v.Userdata()
	if u == nil {
		return nil, false
	}
	if p, ok := u.Data.(*T); ok {
		return p, true
	}
	if x, ok := u.Data.(T); ok {
		p := new(T)
		*p = x
		return p, true
	}
	return nil, false
}
