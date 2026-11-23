package runtime

// Index returns t[k] with full metamethod semantics:
//
//   - tables: raw get, then the __index chain (table or function);
//   - strings: the string library (so ("x"):upper() works);
//   - userdata: __index metamethod, then Go reflection (fields/methods);
//   - anything else: error.
func Index(env *Env, t, k Value) Value {
	switch t.kind {
	case KindTable:
		tb := t.ptr.(*Table)
		if v := tb.Get(k); !v.IsNil() {
			return v
		}
		m := tb.meta
		for m != nil {
			h := m.Get(String("__index"))
			if h.IsNil() {
				return Nil
			}
			switch h.kind {
			case KindTable:
				hb := h.ptr.(*Table)
				if v := hb.Get(k); !v.IsNil() {
					return v
				}
				if hb == tb { // self-referential __index guard
					return Nil
				}
				m = hb.meta
			case KindFunction:
				return Call1(env, h, []Value{t, k})
			case KindUserdata:
				return Index(env, h, k)
			default:
				return Nil
			}
		}
		return Nil
	case KindString:
		return env.strlib.Get(k)
	case KindUserdata:
		u := t.ptr.(*Userdata)
		if u.meta != nil {
			h := u.meta.Get(String("__index"))
			switch h.kind {
			case KindFunction:
				return Call1(env, h, []Value{t, k})
			case KindTable, KindUserdata:
				return Index(env, h, k)
			}
		}
		if v, ok := userdataIndex(env, u, k); ok {
			return v
		}
		return Nil
	}
	Errorf("attempt to index a %s value", t.Type())
	return Nil
}

// SetIndex performs t[k] = v with __newindex semantics: the metamethod only
// fires when the key is absent from the table.
func SetIndex(env *Env, t, k, v Value) {
	switch t.kind {
	case KindTable:
		tb := t.ptr.(*Table)
		if tb.Get(k).IsNil() && tb.meta != nil {
			h := tb.meta.Get(String("__newindex"))
			switch h.kind {
			case KindFunction:
				Call(env, h, []Value{t, k, v})
				return
			case KindTable, KindUserdata:
				SetIndex(env, h, k, v)
				return
			}
		}
		tb.Set(k, v)
		return
	case KindUserdata:
		u := t.ptr.(*Userdata)
		if u.meta != nil {
			h := u.meta.Get(String("__newindex"))
			switch h.kind {
			case KindFunction:
				Call(env, h, []Value{t, k, v})
				return
			case KindTable, KindUserdata:
				SetIndex(env, h, k, v)
				return
			}
		}
		if userdataSetIndex(env, u, k, v) {
			return
		}
		Errorf("attempt to index a %s value", t.Type())
		return
	}
	Errorf("attempt to index a %s value", t.Type())
}

// RawGet returns t[k] without metamethods (rawget).
func RawGet(t, k Value) Value {
	if tb := t.Table(); tb != nil {
		return tb.Get(k)
	}
	Errorf("bad argument #1 to 'rawget' (table expected, got %s)", t.Type())
	return Nil
}

// RawSet performs t[k] = v without metamethods (rawset).
func RawSet(t, k, v Value) {
	if tb := t.Table(); tb != nil {
		tb.Set(k, v)
		return
	}
	Errorf("bad argument #1 to 'rawset' (table expected, got %s)", t.Type())
}

// Len returns #v: string length in bytes, table border, __len metamethod.
func Len(env *Env, v Value) Value {
	switch v.kind {
	case KindString:
		return Int(int64(len(v.str)))
	case KindTable:
		if m := v.ptr.(*Table).meta; m != nil {
			if h := m.Get(String("__len")); h.kind == KindFunction {
				return Call1(env, h, []Value{v})
			}
		}
		return Int(int64(v.ptr.(*Table).Len()))
	case KindUserdata:
		if m := v.ptr.(*Userdata).meta; m != nil {
			if h := m.Get(String("__len")); h.kind == KindFunction {
				return Call1(env, h, []Value{v})
			}
		}
		if r := userdataLen(v.ptr.(*Userdata)); !r.IsNil() {
			return r
		}
	}
	Errorf("attempt to get length of a %s value", v.Type())
	return Nil
}

func concatPart(v Value) (string, bool) {
	switch v.kind {
	case KindString:
		return v.str, true
	case KindNumber:
		return NumToStr(v.num), true
	}
	return "", false
}

// Concat returns a .. b. Strings and numbers concatenate directly; other
// types go through the __concat metamethod or raise an error.
func Concat(env *Env, a, b Value) Value {
	as, aok := concatPart(a)
	bs, bok := concatPart(b)
	if aok && bok {
		return String(as + bs)
	}
	if r := metaBin(env, "__concat", a, b); !r.IsNil() {
		return r
	}
	bad := a
	if aok {
		bad = b
	}
	Errorf("attempt to concatenate a %s value", bad.Type())
	return Nil
}
