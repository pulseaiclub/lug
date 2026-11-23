package runtime

import (
	"sort"
	"strings"
)

func init() { installers = append(installers, installTable) }

// tableArg returns args[i] as a *Table or raises the standard "bad argument"
// error.
func tableArg(name string, args []Value, i int) *Table {
	v := Arg(args, i)
	if t := v.Table(); t != nil {
		return t
	}
	Errorf("bad argument #%d to '%s' (table expected, got %s)", i+1, name, v.Type())
	return nil
}

// tableArgInt coerces args[i] to an integer (Lua's luaL_checkinteger: an
// integral float, or a string convertible to one).
func tableArgInt(name string, args []Value, i int) int64 {
	v := Arg(args, i)
	if n, ok := toInt64(v); ok {
		return n
	}
	if _, ok := ToNumber(v); ok {
		Errorf("bad argument #%d to '%s' (number has no integer representation)", i+1, name)
	}
	Errorf("bad argument #%d to '%s' (number expected, got %s)", i+1, name, v.Type())
	return 0
}

// installTable registers the "table" library and the Lua 5.1 global unpack.
func installTable(e *Env) {
	tbl := NewTable()
	reg := func(name string, fn Fn) { tbl.Set(String(name), NewClosure(fn, "table."+name)) }

	reg("insert", func(env *Env, args []Value) []Value {
		t := tableArg("insert", args, 0)
		n := int64(t.Len())
		switch len(args) {
		case 2: // insert(t, v): append
			t.Set(Number(float64(n+1)), args[1])
		case 3: // insert(t, pos, v): shift up, then place
			pos := tableArgInt("insert", args, 1)
			if pos < 1 || pos > n+1 {
				Errorf("bad argument #2 to 'insert' (position out of bounds)")
			}
			for i := n; i >= pos; i-- {
				t.Set(Number(float64(i+1)), t.Get(Number(float64(i))))
			}
			t.Set(Number(float64(pos)), args[2])
		default:
			Errorf("wrong number of arguments to 'insert'")
		}
		return nil
	})

	reg("remove", func(env *Env, args []Value) []Value {
		t := tableArg("remove", args, 0)
		n := int64(t.Len())
		pos := n
		if !Arg(args, 1).IsNil() {
			pos = tableArgInt("remove", args, 1)
		}
		// A given position is validated unless it is the default one (Lua
		// leaves an empty table's remove() alone). Len()+1 removes nothing.
		if pos != n && (pos < 1 || pos > n+1) {
			Errorf("bad argument #2 to 'remove' (position out of bounds)")
		}
		res := t.Get(Number(float64(pos)))
		for ; pos < n; pos++ { // shift elements down; pos ends at the vacated slot
			t.Set(Number(float64(pos)), t.Get(Number(float64(pos+1))))
		}
		t.Set(Number(float64(pos)), Nil) // no-op when pos == n+1
		return []Value{res}
	})

	reg("concat", func(env *Env, args []Value) []Value {
		t := tableArg("concat", args, 0)
		sep := ""
		if s := Arg(args, 1); !s.IsNil() {
			switch {
			case s.IsString():
				sep = s.Str()
			case s.IsNumber():
				sep = NumToStr(s.Num())
			default:
				Errorf("bad argument #2 to 'concat' (string expected, got %s)", s.Type())
			}
		}
		i := int64(1)
		if !Arg(args, 2).IsNil() {
			i = tableArgInt("concat", args, 2)
		}
		j := int64(t.Len())
		if !Arg(args, 3).IsNil() {
			j = tableArgInt("concat", args, 3)
		}
		tv := TableVal(t)
		var b strings.Builder
		for k := i; k <= j; k++ {
			if k != i {
				b.WriteString(sep)
			}
			v := Index(env, tv, Number(float64(k)))
			switch {
			case v.IsString():
				b.WriteString(v.Str())
			case v.IsNumber():
				b.WriteString(NumToStr(v.Num()))
			default:
				Errorf("invalid value (at index %d) in table for 'concat'", k)
			}
		}
		return []Value{String(b.String())}
	})

	unpack := func(env *Env, args []Value) []Value {
		t := tableArg("unpack", args, 0)
		n := int64(t.Len())
		i := int64(1)
		if !Arg(args, 1).IsNil() {
			i = tableArgInt("unpack", args, 1)
		}
		j := n
		if !Arg(args, 2).IsNil() {
			j = tableArgInt("unpack", args, 2)
		}
		if j < i {
			return nil
		}
		if uint64(j)-uint64(i) >= 1000000 { // Lua's LUAI_MAXCSTACK limit
			Errorf("too many results to unpack")
		}
		out := make([]Value, 0, j-i+1)
		for k := i; k <= j; k++ {
			out = append(out, t.Get(Number(float64(k))))
		}
		return out
	}
	unpackVal := NewClosure(unpack, "unpack")
	tbl.Set(String("unpack"), unpackVal)
	e.Set("unpack", unpackVal)

	reg("pack", func(env *Env, args []Value) []Value {
		t := NewTable()
		for i, v := range args {
			t.Set(Number(float64(i+1)), v)
		}
		t.Set(String("n"), Int(int64(len(args))))
		return []Value{TableVal(t)}
	})

	reg("sort", func(env *Env, args []Value) []Value {
		t := tableArg("sort", args, 0)
		var comp Value // nil: default comparison (LT)
		if c := Arg(args, 1); !c.IsNil() {
			if c.Closure() == nil {
				Errorf("bad argument #2 to 'sort' (function expected, got %s)", c.Type())
			}
			comp = c
		}
		// Read the array part, sort the copy, write it back: the comparator
		// then never observes a half-sorted table.
		items := make([]Value, t.Len())
		for i := range items {
			items[i] = t.Get(Number(float64(i + 1)))
		}
		sort.SliceStable(items, func(x, y int) bool {
			if comp.IsNil() {
				return LT(env, items[x], items[y])
			}
			return Call1(env, comp, []Value{items[x], items[y]}).Truth()
		})
		for i, v := range items {
			t.Set(Number(float64(i+1)), v)
		}
		return nil
	})

	e.Set("table", TableVal(tbl))
}
