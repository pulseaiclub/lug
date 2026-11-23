package runtime

import (
	"math"
	"strings"
	"testing"
	"time"
)

// libStdEnv builds a fresh Env with the full standard library. Other library
// files register installers from their own init functions and New() runs
// them all; if one panics (a sibling file may be mid-write), retry once.
func libStdEnv(t *testing.T) *Env {
	t.Helper()
	newEnv := func() (e *Env, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = WrapPanic(r)
			}
		}()
		e = New()
		return e, nil
	}
	e, err := newEnv()
	if err == nil {
		return e
	}
	time.Sleep(30 * time.Second)
	if e, err = newEnv(); err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	return e
}

// libStdFn looks up lib.name as a function value.
func libStdFn(t *testing.T, e *Env, lib, name string) Value {
	t.Helper()
	f := Index(e, e.Get(lib), String(name))
	if f.Closure() == nil {
		t.Fatalf("%s.%s: expected a function, got %s", lib, name, f.Type())
	}
	return f
}

func libStdCall(t *testing.T, e *Env, lib, name string, args ...Value) Value {
	t.Helper()
	return Call1(e, libStdFn(t, e, lib, name), args)
}

func libStdCallN(t *testing.T, e *Env, lib, name string, args ...Value) []Value {
	t.Helper()
	return Call(e, libStdFn(t, e, lib, name), args)
}

func libStdNum(t *testing.T, got Value, want float64) {
	t.Helper()
	if !got.IsNumber() || got.Num() != want {
		t.Fatalf("got %s, want number %s", ToString(got), NumToStr(want))
	}
}

func libStdClose(t *testing.T, got Value, want float64) {
	t.Helper()
	if !got.IsNumber() || math.Abs(got.Num()-want) > 1e-12*math.Max(1, math.Abs(want)) {
		t.Fatalf("got %s, want approximately %s", ToString(got), NumToStr(want))
	}
}

func libStdStr(t *testing.T, got Value, want string) {
	t.Helper()
	if !got.IsString() || got.Str() != want {
		t.Fatalf("got %s, want string %q", ToString(got), want)
	}
}

// libStdErr asserts that f raises a Lua error whose message contains want.
func libStdErr(t *testing.T, want string, f func()) {
	t.Helper()
	err := Protect(f)
	if err == nil {
		t.Fatalf("expected error containing %q, got none", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %q", want, err.Error())
	}
}

// libStdList builds a table with the given array elements.
func libStdList(vals ...Value) Value {
	tbl := NewTable()
	for i, v := range vals {
		tbl.Set(Number(float64(i+1)), v)
	}
	return TableVal(tbl)
}

// libStdItems asserts that tbl holds exactly the want sequence in its array
// part.
func libStdItems(t *testing.T, tbl Value, want ...Value) {
	t.Helper()
	tb := tbl.Table()
	if tb == nil {
		t.Fatalf("expected a table, got %s", tbl.Type())
	}
	if tb.Len() != len(want) {
		t.Fatalf("table length = %d, want %d", tb.Len(), len(want))
	}
	for i, w := range want {
		if got := tb.Get(Number(float64(i + 1))); !RawEqual(got, w) {
			t.Fatalf("index %d: got %s, want %s", i+1, ToString(got), ToString(w))
		}
	}
}

// libStdDateTable builds an os.time() argument table (hour/min/sec optional).
func libStdDateTable(year, month, day, hour int64) Value {
	tbl := NewTable()
	tbl.Set(String("year"), Int(year))
	tbl.Set(String("month"), Int(month))
	tbl.Set(String("day"), Int(day))
	tbl.Set(String("hour"), Int(hour))
	return TableVal(tbl)
}

func TestMathLib(t *testing.T) {
	e := libStdEnv(t)
	call := func(name string, args ...Value) Value {
		return libStdCall(t, e, "math", name, args...)
	}
	num := func(name string, want float64, args ...Value) {
		t.Helper()
		libStdNum(t, call(name, args...), want)
	}
	closeTo := func(name string, want float64, args ...Value) {
		t.Helper()
		libStdClose(t, call(name, args...), want)
	}

	num("floor", 1, Number(1.5))
	num("floor", -2, Number(-1.5))
	num("floor", 2, String("2.7")) // numeric strings are accepted
	num("ceil", 0, Number(-0.5))
	num("ceil", 2, Number(1.2))
	num("abs", 3, Number(-3))
	num("sqrt", 4, Number(16))
	num("sin", 0, Number(0))
	num("cos", 1, Number(0))
	num("tan", 0, Number(0))
	num("asin", math.Pi/2, Number(1))
	num("acos", 0, Number(1))
	num("atan", math.Pi/4, Number(1))
	closeTo("atan", 3*math.Pi/4, Number(1), Number(-1)) // atan2(1, -1)
	num("pow", 1024, Number(2), Number(10))
	num("fmod", 1, Number(7), Number(3))
	num("fmod", -1, Number(-7), Number(3))
	closeTo("exp", math.E, Number(1))
	closeTo("log", math.Log(8), Number(8))
	closeTo("log", 3, Number(8), Number(2))
	num("max", 5, Number(1), Number(5), Number(3))
	num("max", 10, String("10"), Number(3))
	num("max", 10, Number(10), Number(1), Number(3))
	num("min", 2, Number(4), Number(2), Number(8))
	num("min", -3, String("-3"), Number(2))

	// modf returns the integral and the fractional part
	r := libStdCallN(t, e, "math", "modf", Number(3.7))
	if len(r) != 2 {
		t.Fatalf("modf returned %d values, want 2", len(r))
	}
	libStdNum(t, r[0], 3)
	libStdClose(t, r[1], 0.7)
	r = libStdCallN(t, e, "math", "modf", Number(-3.7))
	libStdNum(t, r[0], -3)
	libStdClose(t, r[1], -0.7)

	// constants
	mathLib := e.Get("math")
	libStdNum(t, Index(e, mathLib, String("pi")), math.Pi)
	libStdNum(t, Index(e, mathLib, String("huge")), math.Inf(1))
	libStdNum(t, Index(e, mathLib, String("maxinteger")), math.MaxInt64)
	libStdNum(t, Index(e, mathLib, String("mininteger")), math.MinInt64)

	// random
	for i := 0; i < 50; i++ {
		v := call("random", Number(5), Number(10))
		if !v.IsNumber() || v.Num() < 5 || v.Num() > 10 || v.Num() != math.Trunc(v.Num()) {
			t.Fatalf("random(5, 10) = %s, want an integer in [5, 10]", ToString(v))
		}
	}
	for i := 0; i < 50; i++ {
		v := call("random", Number(3))
		if !v.IsNumber() || v.Num() < 1 || v.Num() > 3 || v.Num() != math.Trunc(v.Num()) {
			t.Fatalf("random(3) = %s, want an integer in [1, 3]", ToString(v))
		}
	}
	if v := call("random"); !v.IsNumber() || v.Num() < 0 || v.Num() >= 1 {
		t.Fatalf("random() = %s, want a number in [0, 1)", ToString(v))
	}
	seeded := func() float64 {
		call("randomseed", Int(42))
		return call("random", Number(1), Number(1000)).Num()
	}
	if a, b := seeded(), seeded(); a != b {
		t.Fatalf("randomseed(42) is not reproducible: %v vs %v", a, b)
	}
	call("randomseed") // no arguments: reseed randomly

	// errors
	libStdErr(t, "bad argument #1 to 'floor' (number expected, got string)", func() { call("floor", String("x")) })
	libStdErr(t, "bad argument #1 to 'floor' (number expected, got nil)", func() { call("floor") })
	libStdErr(t, "bad argument #2 to 'max' (number expected, got table)", func() { call("max", Number(1), TableVal(NewTable())) })
	libStdErr(t, "bad argument #1 to 'max' (value expected)", func() { call("max") })
	libStdErr(t, "bad argument #2 to 'random' (interval is empty)", func() { call("random", Number(5), Number(2)) })
	libStdErr(t, "bad argument #1 to 'random' (interval is empty)", func() { call("random", Number(0)) })
	libStdErr(t, "bad argument #1 to 'random' (number has no integer representation)", func() { call("random", Number(1.5)) })
	libStdErr(t, "wrong number of arguments to 'random'", func() { call("random", Number(1), Number(2), Number(3)) })
}

func TestTableLib(t *testing.T) {
	e := libStdEnv(t)
	call := func(name string, args ...Value) Value {
		return libStdCall(t, e, "table", name, args...)
	}

	// insert: append, then shift up at a position
	tb := libStdList(Int(1), Int(2), Int(3))
	call("insert", tb, Int(4))
	libStdItems(t, tb, Int(1), Int(2), Int(3), Int(4))
	call("insert", tb, Int(1), Int(0))
	libStdItems(t, tb, Int(0), Int(1), Int(2), Int(3), Int(4))
	call("insert", tb, Int(6), Int(5)) // Len()+1 is allowed
	libStdItems(t, tb, Int(0), Int(1), Int(2), Int(3), Int(4), Int(5))
	tb2 := libStdList(Int(1), Int(2))
	call("insert", tb2, String("1"), Int(0)) // numeric string position
	libStdItems(t, tb2, Int(0), Int(1), Int(2))
	libStdErr(t, "bad argument #2 to 'insert' (position out of bounds)", func() { call("insert", tb, Int(8), Int(9)) }) // Len()+2
	libStdErr(t, "bad argument #2 to 'insert' (position out of bounds)", func() { call("insert", tb, Int(0), Int(9)) })
	libStdErr(t, "wrong number of arguments to 'insert'", func() { call("insert", tb) })
	libStdErr(t, "bad argument #1 to 'insert' (table expected, got number)", func() { call("insert", Int(1), Int(2)) })

	// remove: pops, shifts down, validates positions
	if v := call("remove", tb); !RawEqual(v, Int(5)) {
		t.Fatalf("remove() = %s, want 5", ToString(v))
	}
	libStdItems(t, tb, Int(0), Int(1), Int(2), Int(3), Int(4))
	if v := call("remove", tb, Int(1)); !RawEqual(v, Int(0)) {
		t.Fatalf("remove(t, 1) = %s, want 0", ToString(v))
	}
	libStdItems(t, tb, Int(1), Int(2), Int(3), Int(4))
	if v := call("remove", tb, Int(5)); !v.IsNil() {
		t.Fatalf("remove(t, #t+1) = %s, want nil", ToString(v))
	}
	libStdItems(t, tb, Int(1), Int(2), Int(3), Int(4))
	if v := call("remove", tb, Int(2)); !RawEqual(v, Int(2)) {
		t.Fatalf("remove(t, 2) = %s, want 2", ToString(v))
	}
	libStdItems(t, tb, Int(1), Int(3), Int(4))
	libStdErr(t, "bad argument #2 to 'remove' (position out of bounds)", func() { call("remove", tb, Int(6)) })
	libStdErr(t, "bad argument #2 to 'remove' (position out of bounds)", func() { call("remove", tb, Int(0)) })
	libStdErr(t, "bad argument #1 to 'remove' (table expected, got nil)", func() { call("remove") })
	if v := call("remove", libStdList()); !v.IsNil() {
		t.Fatalf("remove(empty) = %s, want nil", ToString(v))
	}

	// concat
	libStdStr(t, call("concat", libStdList(Int(1), Int(2), Int(3)), String(",")), "1,2,3")
	libStdStr(t, call("concat", libStdList(Int(1), Int(2), Int(3))), "123")
	libStdStr(t, call("concat", libStdList(String("a"), String("b"), String("c")), String("-"), Int(2), Int(3)), "b-c")
	libStdStr(t, call("concat", libStdList(Number(1.5), String("x")), String("|")), "1.5|x")
	libStdStr(t, call("concat", libStdList(Int(1), Int(2)), String(""), Int(2), Int(1)), "")
	// concat reads through __index
	mt := NewTable()
	mt.Set(String("__index"), NewClosure(func(env *Env, args []Value) []Value {
		return []Value{Number(Arg(args, 1).Num() * 10)}
	}, "__index"))
	withMeta := libStdList(Int(1))
	withMeta.Table().SetMetatable(mt)
	libStdStr(t, call("concat", withMeta, String(","), Int(1), Int(3)), "1,20,30")
	libStdErr(t, "invalid value (at index 2) in table for 'concat'", func() {
		call("concat", libStdList(Int(1), TableVal(NewTable()), Int(3)))
	})
	libStdErr(t, "invalid value (at index 3) in table for 'concat'", func() {
		call("concat", libStdList(Int(1), Int(2)), String(""), Int(1), Int(3))
	})
	libStdErr(t, "bad argument #2 to 'concat' (string expected, got table)", func() {
		call("concat", libStdList(Int(1)), TableVal(NewTable()))
	})

	// unpack returns multiple values; the global unpack is table.unpack
	r := libStdCallN(t, e, "table", "unpack", libStdList(Int(1), Int(2), Int(3)))
	if len(r) != 3 || !RawEqual(r[0], Int(1)) || !RawEqual(r[1], Int(2)) || !RawEqual(r[2], Int(3)) {
		t.Fatalf("unpack{1,2,3} = %v, want 1, 2, 3", r)
	}
	r = libStdCallN(t, e, "table", "unpack", libStdList(Int(1), Int(2), Int(3)), Int(2))
	if len(r) != 2 || !RawEqual(r[0], Int(2)) || !RawEqual(r[1], Int(3)) {
		t.Fatalf("unpack(t, 2) = %v, want 2, 3", r)
	}
	r = libStdCallN(t, e, "table", "unpack", libStdList(Int(1)), Int(1), Int(3))
	if len(r) != 3 || !RawEqual(r[0], Int(1)) || !r[1].IsNil() || !r[2].IsNil() {
		t.Fatalf("unpack(t, 1, 3) = %v, want 1, nil, nil", r)
	}
	if r = libStdCallN(t, e, "table", "unpack", libStdList(Int(1)), Int(2), Int(1)); len(r) != 0 {
		t.Fatalf("unpack(t, 2, 1) = %v, want no values", r)
	}
	if e.Get("unpack").Closure() != Index(e, e.Get("table"), String("unpack")).Closure() {
		t.Fatal("global unpack is not the same function as table.unpack")
	}

	// pack
	packed := call("pack", Int(1), Nil, String("three"))
	pt := packed.Table()
	if pt == nil {
		t.Fatalf("pack() returned %s, want a table", packed.Type())
	}
	if v := pt.Get(Number(1)); !RawEqual(v, Int(1)) {
		t.Fatalf("pack()[1] = %s, want 1", ToString(v))
	}
	if v := pt.Get(Number(2)); !v.IsNil() {
		t.Fatalf("pack()[2] = %s, want nil", ToString(v))
	}
	if v := pt.Get(Number(3)); !RawEqual(v, String("three")) {
		t.Fatalf("pack()[3] = %s, want 'three'", ToString(v))
	}
	if v := pt.Get(String("n")); !RawEqual(v, Int(3)) {
		t.Fatalf("pack().n = %s, want 3", ToString(v))
	}

	// sort: default comparison, custom comparator, stability
	tb = libStdList(Int(3), Int(1), Int(2))
	call("sort", tb)
	libStdItems(t, tb, Int(1), Int(2), Int(3))
	tb = libStdList(String("b"), String("a"), String("c"))
	call("sort", tb)
	libStdItems(t, tb, String("a"), String("b"), String("c"))
	desc := NewClosure(func(env *Env, args []Value) []Value {
		return []Value{Bool(Arg(args, 0).Num() > Arg(args, 1).Num())}
	}, "desc")
	tb = libStdList(Int(3), Int(1), Int(2))
	call("sort", tb, desc)
	libStdItems(t, tb, Int(3), Int(2), Int(1))
	pair := func(k int64, tag string) Value {
		p := NewTable()
		p.Set(Number(1), Int(k))
		p.Set(String("tag"), String(tag))
		return TableVal(p)
	}
	byKey := NewClosure(func(env *Env, args []Value) []Value {
		a := Index(env, Arg(args, 0), Number(1))
		b := Index(env, Arg(args, 1), Number(1))
		return []Value{Bool(LT(env, a, b))}
	}, "byKey")
	tb = libStdList(pair(2, "b"), pair(1, "c"), pair(2, "a"))
	call("sort", tb, byKey)
	for i, want := range []string{"c", "b", "a"} {
		if v := Index(e, tb.Table().Get(Number(float64(i+1))), String("tag")); !RawEqual(v, String(want)) {
			t.Fatalf("after stable sort, index %d has tag %s, want %q", i+1, ToString(v), want)
		}
	}
	libStdErr(t, "bad argument #2 to 'sort' (function expected, got string)", func() { call("sort", tb, String("x")) })
	libStdErr(t, "attempt to compare", func() { call("sort", libStdList(Int(1), String("a"))) })
}

func TestOSLib(t *testing.T) {
	e := libStdEnv(t)
	call := func(name string, args ...Value) Value {
		return libStdCall(t, e, "os", name, args...)
	}

	// os.date("*t") describes the current local time (a rollover between the
	// two samples is tolerated) with all fields in range.
	before := time.Now()
	dt := call("date", String("*t"))
	after := time.Now()
	if dt.Table() == nil {
		t.Fatalf("os.date('*t') returned %s, want a table", dt.Type())
	}
	field := func(name string) Value { return Index(e, dt, String(name)) }
	for name, rng := range map[string][2]float64{
		"year": {1970, 9999}, "month": {1, 12}, "day": {1, 31}, "hour": {0, 23},
		"min": {0, 59}, "sec": {0, 60}, "yday": {1, 366}, "wday": {1, 7},
	} {
		v := field(name)
		if !v.IsNumber() || v.Num() < rng[0] || v.Num() > rng[1] || v.Num() != math.Trunc(v.Num()) {
			t.Fatalf("os.date('*t').%s = %s, want an integer in [%g, %g]", name, ToString(v), rng[0], rng[1])
		}
	}
	matches := func(ts time.Time) bool {
		return RawEqual(field("year"), Int(int64(ts.Year()))) && RawEqual(field("month"), Int(int64(ts.Month()))) &&
			RawEqual(field("day"), Int(int64(ts.Day()))) && RawEqual(field("hour"), Int(int64(ts.Hour()))) &&
			RawEqual(field("min"), Int(int64(ts.Minute()))) && RawEqual(field("yday"), Int(int64(ts.YearDay()))) &&
			RawEqual(field("wday"), Int(int64(ts.Weekday())+1))
	}
	if !matches(before) && !matches(after) {
		t.Fatalf("os.date('*t') = %s, does not match local time %v", ToString(field("year")), before.Format(time.RFC1123))
	}

	// fixed instants (the "!" prefix selects UTC)
	libStdStr(t, call("date", String("!%Y-%m-%d"), Int(0)), "1970-01-01")
	libStdStr(t, call("date", String("!%Y-%m-%d %H:%M:%S %A %B %a %b %p %x %X %j %%"), Int(0)),
		"1970-01-01 00:00:00 Thursday January Thu Jan AM 01/01/70 00:00:00 001 %")
	libStdStr(t, call("date", String("!%Y-%m-%dT%H:%M:%S"), Number(1700000000)), "2023-11-14T22:13:20")
	libStdStr(t, call("date", String("!%Y"), String("0")), "1970") // numeric string time
	libStdStr(t, call("date", String("!%c"), Int(0)), "Thu Jan  1 00:00:00 1970")
	libStdStr(t, call("date", String("!%w%u%s"), Int(0)), "440")
	epoch := call("date", String("!*t"), Int(0))
	for name, want := range map[string]int64{
		"year": 1970, "month": 1, "day": 1, "hour": 0, "min": 0, "sec": 0, "yday": 1, "wday": 5,
	} {
		if v := Index(e, epoch, String(name)); !RawEqual(v, Int(want)) {
			t.Fatalf("os.date('!*t', 0).%s = %s, want %d", name, ToString(v), want)
		}
	}
	// default format: the ctime-style string
	before = time.Now()
	def := call("date")
	after = time.Now()
	if !def.IsString() {
		t.Fatalf("os.date() returned %s, want a string", def.Type())
	}
	if s := def.Str(); s != before.Format("Mon Jan _2 15:04:05 2006") && s != after.Format("Mon Jan _2 15:04:05 2006") {
		t.Fatalf("os.date() = %q, want %q", s, before.Format("Mon Jan _2 15:04:05 2006"))
	}
	libStdErr(t, "invalid date format directive '%Q'", func() { call("date", String("%Y%Q")) })
	libStdErr(t, "bad argument #1 to 'date' (string expected, got table)", func() { call("date", TableVal(NewTable())) })
	libStdErr(t, "bad argument #2 to 'date' (number expected, got string)", func() { call("date", String("!%Y"), String("x")) })

	// os.time
	if v := call("time"); !v.IsNumber() || v.Num() <= 0 {
		t.Fatalf("os.time() = %s, want a positive number", ToString(v))
	}
	want := time.Date(2000, time.January, 1, 12, 0, 0, 0, time.Local).Unix()
	libStdNum(t, call("time", libStdDateTable(2000, 1, 1, 12)), float64(want))
	want = time.Date(1999, time.December, 31, 0, 0, 0, 0, time.Local).Unix()
	libStdNum(t, call("time", libStdDateTable(1999, 12, 31, 0)), float64(want)) // hour/min/sec default to 0
	missingYear := libStdDateTable(0, 6, 15, 8)
	missingYear.Table().Set(String("year"), Nil)
	libStdErr(t, `field "year" missing in date table`, func() { call("time", missingYear) })
	libStdErr(t, "bad argument #1 to 'time' (table expected, got string)", func() { call("time", String("x")) })

	// os.getenv
	t.Setenv("LUG_LIB_STD_TEST_VAR", "hello")
	libStdStr(t, call("getenv", String("LUG_LIB_STD_TEST_VAR")), "hello")
	if v := call("getenv", String("LUG_LIB_STD_TEST_UNSET_VAR_7f3a")); !v.IsNil() {
		t.Fatalf("getenv(unset) = %s, want nil", ToString(v))
	}

	// difftime, clock
	libStdNum(t, call("difftime", Number(10), Number(4)), 6)
	libStdNum(t, call("difftime", Int(100), String("90")), 10)
	if v := call("clock"); !v.IsNumber() || v.Num() < 0 || v.Num() > 3600 {
		t.Fatalf("os.clock() = %s, want seconds since process start", ToString(v))
	}
}
