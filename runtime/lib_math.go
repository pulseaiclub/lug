package runtime

import (
	"math"
	"math/rand/v2"
)

// mathRand is the generator behind math.random. It is package-local and is
// only ever used from one goroutine at a time (the runtime serializes
// coroutines), so math.randomseed can replace it wholesale.
var mathRand = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))

func init() { installers = append(installers, installMath) }

// mathNum coerces args[i] to a float64 for the math library. Numbers and
// numeric strings are accepted; anything else raises the standard
// "bad argument" error.
func mathNum(name string, args []Value, i int) float64 {
	v := Arg(args, i)
	if f, ok := ToNumber(v); ok {
		return f
	}
	Errorf("bad argument #%d to '%s' (number expected, got %s)", i+1, name, v.Type())
	return 0
}

// mathInt is mathNum for arguments that must be integers (random,
// randomseed), following Lua's luaL_checkinteger.
func mathInt(name string, args []Value, i int) int64 {
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

// mathRandInt returns a uniform integer in [lo, hi] (lo <= hi).
func mathRandInt(lo, hi int64) int64 {
	span := uint64(hi) - uint64(lo) + 1 // number of integers; 0 means the full 2^64 range
	off := uint64(0)
	if span == 0 {
		off = mathRand.Uint64()
	} else {
		off = mathRand.Uint64N(span)
	}
	return int64(uint64(lo) + off)
}

// installMath registers the "math" library.
func installMath(e *Env) {
	m := NewTable()
	reg := func(name string, fn Fn) { m.Set(String(name), NewClosure(fn, "math."+name)) }

	unary := func(name string, f func(float64) float64) {
		reg(name, func(env *Env, args []Value) []Value {
			return []Value{Number(f(mathNum(name, args, 0)))}
		})
	}
	unary("abs", math.Abs)
	unary("ceil", math.Ceil)
	unary("floor", math.Floor)
	unary("sqrt", math.Sqrt)
	unary("exp", math.Exp)
	unary("sin", math.Sin)
	unary("cos", math.Cos)
	unary("tan", math.Tan)
	unary("asin", math.Asin)
	unary("acos", math.Acos)

	binary := func(name string, f func(x, y float64) float64) {
		reg(name, func(env *Env, args []Value) []Value {
			return []Value{Number(f(mathNum(name, args, 0), mathNum(name, args, 1)))}
		})
	}
	binary("fmod", math.Mod) // C fmod
	binary("pow", math.Pow)  // kept for Lua 5.1 compatibility

	reg("atan", func(env *Env, args []Value) []Value {
		y := mathNum("atan", args, 0)
		if x := Arg(args, 1); !x.IsNil() {
			return []Value{Number(math.Atan2(y, mathNum("atan", args, 1)))}
		}
		return []Value{Number(math.Atan(y))}
	})

	reg("log", func(env *Env, args []Value) []Value {
		x := mathNum("log", args, 0)
		if base := Arg(args, 1); !base.IsNil() {
			return []Value{Number(math.Log(x) / math.Log(mathNum("log", args, 1)))}
		}
		return []Value{Number(math.Log(x))}
	})

	reg("modf", func(env *Env, args []Value) []Value {
		ip, fp := math.Modf(mathNum("modf", args, 0))
		return []Value{Number(ip), Number(fp)}
	})

	minmax := func(name string, better func(a, b float64) bool) Fn {
		return func(env *Env, args []Value) []Value {
			if len(args) == 0 {
				Errorf("bad argument #1 to '%s' (value expected)", name)
			}
			best := mathNum(name, args, 0)
			for i := 1; i < len(args); i++ {
				if v := mathNum(name, args, i); better(v, best) {
					best = v
				}
			}
			return []Value{Number(best)}
		}
	}
	reg("max", minmax("max", func(a, b float64) bool { return a > b }))
	reg("min", minmax("min", func(a, b float64) bool { return a < b }))

	reg("random", func(env *Env, args []Value) []Value {
		switch len(args) {
		case 0:
			return []Value{Number(mathRand.Float64())}
		case 1:
			hi := mathInt("random", args, 0)
			if hi < 1 {
				Errorf("bad argument #1 to 'random' (interval is empty)")
			}
			return []Value{Number(float64(mathRandInt(1, hi)))}
		case 2:
			lo, hi := mathInt("random", args, 0), mathInt("random", args, 1)
			if lo > hi {
				Errorf("bad argument #2 to 'random' (interval is empty)")
			}
			return []Value{Number(float64(mathRandInt(lo, hi)))}
		}
		Errorf("wrong number of arguments to 'random'")
		return nil
	})

	reg("randomseed", func(env *Env, args []Value) []Value {
		// randomseed(x) is randomseed(x, 0); no arguments reseed randomly.
		var s1, s2 uint64
		if Arg(args, 0).IsNil() {
			s1, s2 = rand.Uint64(), rand.Uint64()
		} else {
			s1 = uint64(mathInt("randomseed", args, 0))
			if !Arg(args, 1).IsNil() {
				s2 = uint64(mathInt("randomseed", args, 1))
			}
		}
		mathRand = rand.New(rand.NewPCG(s1, s2))
		return nil
	})

	m.Set(String("huge"), Number(math.Inf(1)))
	m.Set(String("pi"), Number(math.Pi))
	m.Set(String("maxinteger"), Number(math.MaxInt64))
	m.Set(String("mininteger"), Number(math.MinInt64))

	e.Set("math", TableVal(m))
}
