package runtime

import (
	"os"
	"strings"
)

func init() { installers = append(installers, installBase) }

// reg registers fn as a global.
func reg(e *Env, name string, fn Fn) { e.Set(name, NewClosure(fn, name)) }

// installBase installs the base library (print, pcall, pairs, ...).
func installBase(e *Env) {
	reg(e, "print", basePrint)
	reg(e, "type", baseType)
	reg(e, "tostring", baseTostring)
	reg(e, "tonumber", baseTonumber)
	reg(e, "error", baseError)
	reg(e, "pcall", basePcall)
	reg(e, "xpcall", baseXpcall)
	reg(e, "assert", baseAssert)
	reg(e, "select", baseSelect)
	reg(e, "next", baseNext)
	reg(e, "pairs", basePairs)
	reg(e, "ipairs", baseIpairs)
	reg(e, "rawget", baseRawget)
	reg(e, "rawset", baseRawset)
	reg(e, "rawequal", baseRawequal)
	reg(e, "rawlen", baseRawlen)
	reg(e, "setmetatable", baseSetmetatable)
	reg(e, "getmetatable", baseGetmetatable)
	reg(e, "collectgarbage", baseCollectgarbage)

	// _G / _ENV refer to the globals table.
	e.Set("_G", e.Globals())
	e.Set("_ENV", e.Globals())
}

func basePrint(env *Env, args []Value) []Value {
	var sb strings.Builder
	for i, a := range args {
		if i > 0 {
			sb.WriteByte('\t')
		}
		sb.WriteString(ToStringEnv(env, a))
	}
	sb.WriteByte('\n')
	os.Stdout.WriteString(sb.String())
	return nil
}

func baseType(env *Env, args []Value) []Value {
	return []Value{String(Arg(args, 0).Type())}
}

func baseTostring(env *Env, args []Value) []Value {
	return []Value{String(ToStringEnv(env, Arg(args, 0)))}
}

func baseTonumber(env *Env, args []Value) []Value {
	v := Arg(args, 0)
	if len(args) >= 2 && !args[1].IsNil() {
		base, ok := ToNumber(args[1])
		if !ok || base < 2 || base > 36 || base != float64(int64(base)) {
			Errorf("bad argument #2 to 'tonumber' (base out of range)")
		}
		s, ok := strOf(v)
		if !ok {
			return []Value{Nil}
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return []Value{Nil}
		}
		neg := false
		if s[0] == '-' || s[0] == '+' {
			neg = s[0] == '-'
			s = s[1:]
		}
		var n float64
		b := int64(base)
		for i := 0; i < len(s); i++ {
			d := digitVal(s[i])
			if d < 0 || int64(d) >= b {
				return []Value{Nil}
			}
			n = n*float64(b) + float64(d)
		}
		if neg {
			n = -n
		}
		return []Value{Number(n)}
	}
	if v.kind == KindNumber {
		return []Value{v}
	}
	if f, ok := ToNumber(v); ok {
		return []Value{Number(f)}
	}
	return []Value{Nil}
}

func strOf(v Value) (string, bool) {
	if v.kind == KindString {
		return v.str, true
	}
	return "", false
}

func digitVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return -1
}

func baseError(env *Env, args []Value) []Value {
	v := Arg(args, 0)
	if v.IsNil() {
		v = String("error")
	}
	// Lua prepends position info when level != 0; transpiled code has no
	// Lua line information, so the message is raised as-is.
	Error(v)
	return nil
}

func basePcall(env *Env, args []Value) []Value {
	f := Arg(args, 0)
	var rets []Value
	if err := Protect(func() { rets = Call(env, f, args[min(1, len(args)):]) }); err != nil {
		return []Value{False, err.Value}
	}
	return append([]Value{True}, rets...)
}

func baseXpcall(env *Env, args []Value) []Value {
	f := Arg(args, 0)
	handler := Arg(args, 1)
	var rets []Value
	if err := Protect(func() { rets = Call(env, f, args[min(2, len(args)):]) }); err != nil {
		var msg []Value
		if herr := Protect(func() { msg = Call(env, handler, []Value{err.Value}) }); herr != nil {
			return []Value{False, herr.Value}
		}
		if len(msg) > 0 {
			return []Value{False, msg[0]}
		}
		return []Value{False, String("error object is not a string")}
	}
	return append([]Value{True}, rets...)
}

func baseAssert(env *Env, args []Value) []Value {
	if !Arg(args, 0).Truth() {
		if len(args) >= 2 {
			Error(args[1])
		}
		ErrorString("assertion failed!")
	}
	return args
}

func baseSelect(env *Env, args []Value) []Value {
	n := Arg(args, 0)
	if s, ok := strOf(n); ok && s == "#" {
		if len(args) < 1 {
			return []Value{Int(0)}
		}
		return []Value{Int(int64(len(args) - 1))}
	}
	idx, ok := ToNumber(n)
	if !ok {
		Errorf("bad argument #1 to 'select' (number expected, got %s)", n.Type())
	}
	i := int64(idx)
	if i < 0 {
		i = int64(len(args)) + i
	}
	if i < 1 {
		Errorf("bad argument #1 to 'select' (index out of range)")
	}
	if int(i) >= len(args) {
		return nil
	}
	return args[i:]
}

func baseNext(env *Env, args []Value) []Value {
	t := Arg(args, 0)
	k := Arg(args, 1)
	tb := t.Table()
	if tb == nil {
		Errorf("bad argument #1 to 'next' (table expected, got %s)", t.Type())
	}
	nk, nv, ok := tb.Next(k)
	if !ok {
		return []Value{Nil}
	}
	return []Value{nk, nv}
}

func basePairs(env *Env, args []Value) []Value {
	t := Arg(args, 0)
	if m := MetaOf(t); m != nil {
		if h := m.Get(String("__pairs")); h.kind == KindFunction {
			return Call(env, h, []Value{t})
		}
	}
	if t.Table() == nil {
		Errorf("bad argument #1 to 'pairs' (table expected, got %s)", t.Type())
	}
	return []Value{NewClosure(baseNext, "next"), t, Nil}
}

func baseIpairs(env *Env, args []Value) []Value {
	t := Arg(args, 0)
	if t.Table() == nil {
		Errorf("bad argument #1 to 'ipairs' (table expected, got %s)", t.Type())
	}
	iter := NewClosure(func(env *Env, args []Value) []Value {
		t := Arg(args, 0)
		i, _ := ToNumber(Arg(args, 1))
		v := Index(env, t, Number(i+1))
		if v.IsNil() {
			return []Value{Nil}
		}
		return []Value{Number(i + 1), v}
	}, "ipairs")
	return []Value{iter, t, Int(0)}
}

func baseRawget(env *Env, args []Value) []Value {
	return []Value{RawGet(Arg(args, 0), Arg(args, 1))}
}

func baseRawset(env *Env, args []Value) []Value {
	RawSet(Arg(args, 0), Arg(args, 1), Arg(args, 2))
	return args[:1]
}

func baseRawequal(env *Env, args []Value) []Value {
	return []Value{Bool(RawEqual(Arg(args, 0), Arg(args, 1)))}
}

func baseRawlen(env *Env, args []Value) []Value {
	v := Arg(args, 0)
	switch v.kind {
	case KindString:
		return []Value{Int(int64(len(v.str)))}
	case KindTable:
		return []Value{Int(int64(v.ptr.(*Table).Len()))}
	case KindUserdata:
		if r := userdataLen(v.ptr.(*Userdata)); !r.IsNil() {
			return []Value{r}
		}
	}
	Errorf("bad argument #1 to 'rawlen' (table or string expected, got %s)", v.Type())
	return nil
}

func baseSetmetatable(env *Env, args []Value) []Value {
	t := Arg(args, 0)
	tb := t.Table()
	if tb == nil {
		Errorf("bad argument #1 to 'setmetatable' (table expected, got %s)", t.Type())
	}
	m := Arg(args, 1)
	var mt *Table
	if !m.IsNil() {
		mt = m.Table()
		if mt == nil {
			Errorf("bad argument #2 to 'setmetatable' (nil or table expected, got %s)", m.Type())
		}
	}
	if tb.meta != nil {
		if p := tb.meta.Get(String("__metatable")); !p.IsNil() {
			ErrorString("cannot change a protected metatable")
		}
	}
	tb.SetMetatable(mt)
	return []Value{t}
}

func baseGetmetatable(env *Env, args []Value) []Value {
	m := MetaOf(Arg(args, 0))
	if m == nil {
		return []Value{Nil}
	}
	if p := m.Get(String("__metatable")); !p.IsNil() {
		return []Value{p}
	}
	return []Value{TableVal(m)}
}

func baseCollectgarbage(env *Env, args []Value) []Value {
	return []Value{Int(0)}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
