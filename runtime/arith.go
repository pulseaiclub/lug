package runtime

import "math"

// toNum applies Lua arithmetic coercion: numbers pass through, strings are
// parsed, everything else fails.
func toNum(v Value) (float64, bool) {
	if v.kind == KindNumber {
		return v.num, true
	}
	if v.kind == KindString {
		return StrToNum(v.str)
	}
	return 0, false
}

// metaBin looks up a binary metamethod on a, then on b, and calls it with
// (a, b). Returns Nil when neither operand has the metamethod.
func metaBin(env *Env, event string, a, b Value) Value {
	if m := MetaOf(a); m != nil {
		if h := m.Get(String(event)); h.kind == KindFunction {
			return Call1(env, h, []Value{a, b})
		}
	}
	if m := MetaOf(b); m != nil {
		if h := m.Get(String(event)); h.kind == KindFunction {
			return Call1(env, h, []Value{a, b})
		}
	}
	return Nil
}

// metaUn looks up a unary metamethod on v.
func metaUn(env *Env, event string, v Value) Value {
	if m := MetaOf(v); m != nil {
		if h := m.Get(String(event)); h.kind == KindFunction {
			return Call1(env, h, []Value{v})
		}
	}
	return Nil
}

//go:noinline
func arithOp(env *Env, event string, a, b Value, f func(x, y float64) Value) Value {
	x, ok1 := toNum(a)
	y, ok2 := toNum(b)
	if ok1 && ok2 {
		return f(x, y)
	}
	if r := metaBin(env, event, a, b); !r.IsNil() {
		return r
	}
	bad := a
	if ok1 {
		bad = b
	}
	Errorf("attempt to perform arithmetic on a %s value", bad.Type())
	return Nil
}

// The arithmetic helpers below all start with a numeric fast path: when both
// operands are numbers the result is computed directly, and only mixed or
// non-numeric operands fall back to the coercing, metamethod-aware path.

// Slow paths. They are marked noinline so the fast paths stay within the
// inliner budget: numeric code then compiles to a couple of comparisons
// instead of an out-of-line call.
//
//go:noinline
func addSlow(env *Env, a, b Value) Value { return arithOp(env, "__add", a, b, addf) }

//go:noinline
func subSlow(env *Env, a, b Value) Value { return arithOp(env, "__sub", a, b, subf) }

//go:noinline
func mulSlow(env *Env, a, b Value) Value { return arithOp(env, "__mul", a, b, mulf) }

//go:noinline
func divSlow(env *Env, a, b Value) Value { return arithOp(env, "__div", a, b, divf) }

//go:noinline
func modSlow(env *Env, a, b Value) Value { return arithOp(env, "__mod", a, b, modf) }

//go:noinline
func powSlow(env *Env, a, b Value) Value { return arithOp(env, "__pow", a, b, powf) }

//go:noinline
func iDivSlow(env *Env, a, b Value) Value { return arithOp(env, "__idiv", a, b, idivf) }

func addf(x, y float64) Value  { return Number(x + y) }
func subf(x, y float64) Value  { return Number(x - y) }
func mulf(x, y float64) Value  { return Number(x * y) }
func divf(x, y float64) Value  { return Number(x / y) }
func modf(x, y float64) Value  { return Number(x - math.Floor(x/y)*y) }
func powf(x, y float64) Value  { return Number(math.Pow(x, y)) }
func idivf(x, y float64) Value { return Number(math.Floor(x / y)) }

// Float helpers for the arithmetic fast path, where both operands are
// known to be numbers at compile time.
func ModNum(x, y float64) float64  { return x - math.Floor(x/y)*y }
func PowNum(x, y float64) float64  { return math.Pow(x, y) }
func IDivNum(x, y float64) float64 { return math.Floor(x / y) }

// NumVal coerces v to a float64, raising a Lua error when it is not a
// number. It backs statically typed locals on the rare path where a value
// arrives from an untyped source.
func NumVal(v Value) float64 {
	if v.kind == KindNumber {
		return v.num
	}
	if x, ok := toNum(v); ok {
		return x
	}
	Errorf("attempt to use a %s value as a number", v.Type())
	return 0
}

// Add returns a + b.
func Add(env *Env, a, b Value) Value {
	if a.kind == KindNumber && b.kind == KindNumber {
		return Number(a.num + b.num)
	}
	return addSlow(env, a, b)
}

// Sub returns a - b.
func Sub(env *Env, a, b Value) Value {
	if a.kind == KindNumber && b.kind == KindNumber {
		return Number(a.num - b.num)
	}
	return subSlow(env, a, b)
}

// Mul returns a * b.
func Mul(env *Env, a, b Value) Value {
	if a.kind == KindNumber && b.kind == KindNumber {
		return Number(a.num * b.num)
	}
	return mulSlow(env, a, b)
}

// Div returns a / b (always float division).
func Div(env *Env, a, b Value) Value {
	if a.kind == KindNumber && b.kind == KindNumber {
		return Number(a.num / b.num)
	}
	return divSlow(env, a, b)
}

// Mod returns a % b with Lua semantics: a - floor(a/b)*b.
func Mod(env *Env, a, b Value) Value {
	if a.kind == KindNumber && b.kind == KindNumber {
		return Number(a.num - math.Floor(a.num/b.num)*b.num)
	}
	return modSlow(env, a, b)
}

// Pow returns a ^ b.
func Pow(env *Env, a, b Value) Value {
	if a.kind == KindNumber && b.kind == KindNumber {
		return Number(math.Pow(a.num, b.num))
	}
	return powSlow(env, a, b)
}

// IDiv returns a // b (floor division).
func IDiv(env *Env, a, b Value) Value {
	if a.kind == KindNumber && b.kind == KindNumber {
		return Number(math.Floor(a.num / b.num))
	}
	return iDivSlow(env, a, b)
}

// Unm returns -v.
func Unm(env *Env, v Value) Value {
	if v.kind == KindNumber {
		return Number(-v.num)
	}
	if x, ok := toNum(v); ok {
		return Number(-x)
	}
	if r := metaUn(env, "__unm", v); !r.IsNil() {
		return r
	}
	Errorf("attempt to perform arithmetic on a %s value (unary '-')", v.Type())
	return Nil
}

// toInt64 converts a value to an int64 with Lua integer-representation
// rules (integral float in range, or a string convertible to one).
func toInt64(v Value) (int64, bool) {
	if v.kind == KindNumber {
		if v.num == math.Trunc(v.num) && v.num >= math.MinInt64 && v.num < math.MaxInt64 {
			return int64(v.num), true
		}
		return 0, false
	}
	if v.kind == KindString {
		if f, ok := StrToNum(v.str); ok {
			return toInt64(Number(f))
		}
	}
	return 0, false
}

func bitOp(env *Env, event string, a, b Value, f func(x, y int64) Value) Value {
	x, ok1 := toInt64(a)
	y, ok2 := toInt64(b)
	if ok1 && ok2 {
		return f(x, y)
	}
	if r := metaBin(env, event, a, b); !r.IsNil() {
		return r
	}
	Errorf("number has no integer representation")
	return Nil
}

func shl(x, y int64) int64 {
	if y >= 0 {
		if y >= 64 {
			return 0
		}
		return int64(uint64(x) << uint(y))
	}
	if y <= -64 {
		return 0
	}
	return int64(uint64(x) >> uint(-y))
}

func shr(x, y int64) int64 {
	if y >= 0 {
		if y >= 64 {
			return 0
		}
		return int64(uint64(x) >> uint(y))
	}
	if y <= -64 {
		return 0
	}
	return int64(uint64(x) << uint(-y))
}

// BitAnd returns a & b.
func BitAnd(env *Env, a, b Value) Value {
	return bitOp(env, "__band", a, b, func(x, y int64) Value { return Number(float64(x & y)) })
}

// BitOr returns a | b.
func BitOr(env *Env, a, b Value) Value {
	return bitOp(env, "__bor", a, b, func(x, y int64) Value { return Number(float64(x | y)) })
}

// BitXor returns a ~ b.
func BitXor(env *Env, a, b Value) Value {
	return bitOp(env, "__bxor", a, b, func(x, y int64) Value { return Number(float64(x ^ y)) })
}

// Shl returns a << b.
func Shl(env *Env, a, b Value) Value {
	return bitOp(env, "__shl", a, b, func(x, y int64) Value { return Number(float64(shl(x, y))) })
}

// Shr returns a >> b.
func Shr(env *Env, a, b Value) Value {
	return bitOp(env, "__shr", a, b, func(x, y int64) Value { return Number(float64(shr(x, y))) })
}

// BitNot returns ~v.
func BitNot(env *Env, v Value) Value {
	if x, ok := toInt64(v); ok {
		return Number(float64(^x))
	}
	if r := metaUn(env, "__bnot", v); !r.IsNil() {
		return r
	}
	Errorf("number has no integer representation")
	return Nil
}
