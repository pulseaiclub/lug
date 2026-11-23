package runtime

// LT reports a < b. Numbers and strings compare directly; other types go
// through the __lt metamethod or raise an error.
func LT(env *Env, a, b Value) bool {
	if a.kind == KindNumber && b.kind == KindNumber {
		return a.num < b.num
	}
	if a.kind == KindString && b.kind == KindString {
		return a.str < b.str
	}
	if r := metaBin(env, "__lt", a, b); !r.IsNil() {
		return r.Truth()
	}
	Errorf("attempt to compare %s with %s", a.Type(), b.Type())
	return false
}

// LE reports a <= b.
func LE(env *Env, a, b Value) bool {
	if a.kind == KindNumber && b.kind == KindNumber {
		return a.num <= b.num
	}
	if a.kind == KindString && b.kind == KindString {
		return a.str <= b.str
	}
	if r := metaBin(env, "__le", a, b); !r.IsNil() {
		return r.Truth()
	}
	Errorf("attempt to compare %s with %s", a.Type(), b.Type())
	return false
}

// EQ reports a == b. Tables and userdata compare by identity first, then
// through the __eq metamethod; other values compare primitively.
func EQ(env *Env, a, b Value) bool {
	if a.kind == b.kind {
		switch a.kind {
		case KindTable, KindUserdata:
			if RawEqual(a, b) {
				return true
			}
			if r := metaBin(env, "__eq", a, b); !r.IsNil() {
				return r.Truth()
			}
			return false
		}
	}
	return RawEqual(a, b)
}
