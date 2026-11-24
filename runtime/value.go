// Package runtime implements the Lua value model, environment and standard
// library that the interpreter runs on.
//
// Host programs normally use the top-level lug package instead, which
// re-exports the types and constructors needed at the API boundary. This
// package is public because a host may want the pieces directly: value kinds,
// tables, metatables and coroutines.
//
// Design notes:
//
//   - Value is a tagged union. Numbers and booleans are stored inline and
//     never allocate. Strings are immutable Go strings. Tables, closures,
//     userdata and coroutines are stored as pointers.
//   - Numbers are float64 only (Lua 5.1 / LuaJIT numeric semantics) with
//     Lua 5.4 syntax and operator semantics.
//   - All runtime type errors panic with *LuaError, exactly like Lua's
//     error(); pcall recovers it at the boundary.
package runtime

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Kind is the type tag of a Value.
type Kind uint8

const (
	KindNil Kind = iota
	KindBool
	KindNumber
	KindString
	KindTable
	KindFunction
	KindUserdata
	KindThread
)

func (k Kind) String() string {
	switch k {
	case KindNil:
		return "nil"
	case KindBool:
		return "boolean"
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindTable:
		return "table"
	case KindFunction:
		return "function"
	case KindUserdata:
		return "userdata"
	case KindThread:
		return "thread"
	default:
		return "unknown"
	}
}

// Value is a Lua value. The zero Value is nil.
type Value struct {
	kind Kind
	num  float64 // number payload / bool payload
	str  string  // string payload
	ptr  any     // *Table, *Closure, *Userdata or *Coroutine
}

var (
	// Nil is the nil value.
	Nil = Value{}
	// True is the boolean true.
	True = Value{kind: KindBool, num: 1}
	// False is the boolean false.
	False = Value{kind: KindBool}
)

// Number returns a number Value.
func Number(f float64) Value { return Value{kind: KindNumber, num: f} }

// Int returns a number Value from an integer.
func Int(i int64) Value { return Value{kind: KindNumber, num: float64(i)} }

// String returns a string Value.
func String(s string) Value { return Value{kind: KindString, str: s} }

// Bool returns a boolean Value.
func Bool(b bool) Value {
	if b {
		return True
	}
	return False
}

// TableVal returns a table Value wrapping t.
func TableVal(t *Table) Value { return Value{kind: KindTable, ptr: t} }

// Kind returns the value's kind.
func (v Value) Kind() Kind { return v.kind }

// Type returns the Lua type name of the value.
func (v Value) Type() string { return v.kind.String() }

// IsNil reports whether v is nil.
func (v Value) IsNil() bool { return v.kind == KindNil }

// Truth reports Lua truthiness: nil and false are false, all else is true.
// (Package-level form, for callers that hold a Value without importing the
// method.)
func Truth(v Value) bool { return v.Truth() }

// Truth reports Lua truthiness: nil and false are false, all else is true.
func (v Value) Truth() bool {
	if v.kind == KindNil {
		return false
	}
	if v.kind == KindBool {
		return v.num != 0
	}
	return true
}

// IsNumber reports whether v is a number.
func (v Value) IsNumber() bool { return v.kind == KindNumber }

// IsString reports whether v is a string.
func (v Value) IsString() bool { return v.kind == KindString }

// Num returns the numeric payload. Only meaningful when IsNumber.
func (v Value) Num() float64 { return v.num }

// Str returns the string payload. Only meaningful when IsString.
func (v Value) Str() string { return v.str }

// BoolVal returns the boolean payload. Only meaningful for KindBool.
func (v Value) BoolVal() bool { return v.num != 0 }

// Table returns the table pointer, or nil if v is not a table.
func (v Value) Table() *Table {
	if v.kind == KindTable {
		return v.ptr.(*Table)
	}
	return nil
}

// Closure returns the closure, or nil if v is not a function.
func (v Value) Closure() *Closure {
	if v.kind == KindFunction {
		return v.ptr.(*Closure)
	}
	return nil
}

// Userdata returns the userdata, or nil if v is not userdata.
func (v Value) Userdata() *Userdata {
	if v.kind == KindUserdata {
		return v.ptr.(*Userdata)
	}
	return nil
}

// NumToStr formats a float64 exactly like Lua's tostring: "%.14g",
// with C-style inf/nan spellings.
func NumToStr(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	return strconv.FormatFloat(f, 'g', 14, 64)
}

// ToString converts a value to its Lua string form without invoking
// metamethods (use ToStringEnv for the __tostring metamethod).
func ToString(v Value) string {
	switch v.kind {
	case KindNil:
		return "nil"
	case KindBool:
		if v.num != 0 {
			return "true"
		}
		return "false"
	case KindNumber:
		return NumToStr(v.num)
	case KindString:
		return v.str
	case KindTable:
		return fmt.Sprintf("table: %p", v.ptr)
	case KindFunction:
		return fmt.Sprintf("function: %p", v.ptr)
	case KindUserdata:
		return fmt.Sprintf("userdata: %p", v.ptr)
	case KindThread:
		return fmt.Sprintf("thread: %p", v.ptr)
	}
	return "?"
}

// ToNumber converts a value to float64 using Lua coercion rules: numbers
// pass through; strings are parsed; everything else fails.
func ToNumber(v Value) (float64, bool) {
	switch v.kind {
	case KindNumber:
		return v.num, true
	case KindString:
		return StrToNum(v.str)
	}
	return 0, false
}

// StrToNum parses a Lua number literal: decimal, hex ("0x1A"),
// hex floats ("0x1p-2") and optional surrounding whitespace.
func StrToNum(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	body := s
	neg := false
	if body[0] == '+' {
		body = body[1:]
	} else if body[0] == '-' {
		neg = true
		body = body[1:]
	}
	if len(body) > 1 && body[0] == '0' && (body[1] == 'x' || body[1] == 'X') {
		f, ok := hexToFloat(body)
		if !ok {
			return 0, false
		}
		if neg {
			f = -f
		}
		return f, true
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func hexToFloat(s string) (float64, bool) {
	// s starts with "0x".
	mant := s[2:]
	exp := 0
	if i := strings.IndexAny(mant, "pP"); i >= 0 {
		e, err := strconv.Atoi(mant[i+1:])
		if err != nil {
			return 0, false
		}
		exp = e
		mant = mant[:i]
	}
	intPart, fracPart := mant, ""
	if i := strings.IndexByte(mant, '.'); i >= 0 {
		intPart, fracPart = mant[:i], mant[i+1:]
	}
	if intPart == "" && fracPart == "" {
		return 0, false
	}
	ival, err := strconv.ParseUint(intPart, 16, 64)
	if err != nil {
		return 0, false
	}
	f := float64(ival)
	scale := 1.0 / 16.0
	for i := 0; i < len(fracPart); i++ {
		d := hexDigit(fracPart[i])
		if d < 0 {
			return 0, false
		}
		f += float64(d) * scale
		scale /= 16
	}
	return f * math.Pow(2, float64(exp)), true
}

func hexDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// RawEqual reports primitive (non-metamethod) equality.
func RawEqual(a, b Value) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case KindNil:
		return true
	case KindBool, KindNumber:
		return a.num == b.num
	case KindString:
		return a.str == b.str
	default:
		return a.ptr == b.ptr
	}
}
