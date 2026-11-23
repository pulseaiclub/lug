package runtime

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// osStart is the process-start proxy used by os.clock.
var osStart = time.Now()

func init() { installers = append(installers, installOS) }

// osNum coerces args[i] to a float64 (numbers and numeric strings).
func osNum(name string, args []Value, i int) float64 {
	v := Arg(args, i)
	if f, ok := ToNumber(v); ok {
		return f
	}
	Errorf("bad argument #%d to '%s' (number expected, got %s)", i+1, name, v.Type())
	return 0
}

// osInt coerces args[i] to an integer number of seconds (a time_t),
// truncating like C's cast and rejecting values outside int64.
func osInt(name string, args []Value, i int) int64 {
	f := osNum(name, args, i)
	if math.IsNaN(f) || f < math.MinInt64 || f >= math.MaxInt64 {
		Errorf("bad argument #%d to '%s' (number has no integer representation)", i+1, name)
	}
	return int64(f)
}

// osStr coerces args[i] to a string (Lua's luaL_checkstring also accepts
// numbers).
func osStr(name string, args []Value, i int) string {
	v := Arg(args, i)
	switch {
	case v.IsString():
		return v.Str()
	case v.IsNumber():
		return NumToStr(v.Num())
	}
	Errorf("bad argument #%d to '%s' (string expected, got %s)", i+1, name, v.Type())
	return ""
}

// osDateTable builds the table returned by os.date("*t").
func osDateTable(ts time.Time) *Table {
	t := NewTable()
	set := func(name string, v Value) { t.Set(String(name), v) }
	set("year", Int(int64(ts.Year())))
	set("month", Int(int64(ts.Month())))
	set("day", Int(int64(ts.Day())))
	set("hour", Int(int64(ts.Hour())))
	set("min", Int(int64(ts.Minute())))
	set("sec", Int(int64(ts.Second())))
	set("yday", Int(int64(ts.YearDay())))
	set("wday", Int(int64(ts.Weekday())+1)) // 1 = Sunday .. 7 = Saturday
	set("isdst", Bool(ts.IsDST()))
	return t
}

// osFormatDate expands a strftime-like format string for ts. Each directive
// maps to a Go time.Format layout piece; unknown directives are errors.
func osFormatDate(format string, ts time.Time) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(format) { // dangling '%'
			b.WriteByte('%')
			break
		}
		switch format[i] {
		case 'Y':
			b.WriteString(ts.Format("2006"))
		case 'y':
			b.WriteString(ts.Format("06"))
		case 'm':
			b.WriteString(ts.Format("01"))
		case 'd':
			b.WriteString(ts.Format("02"))
		case 'e':
			b.WriteString(ts.Format("_2"))
		case 'j':
			b.WriteString(ts.Format("002"))
		case 'H':
			b.WriteString(ts.Format("15"))
		case 'I':
			b.WriteString(ts.Format("03"))
		case 'M':
			b.WriteString(ts.Format("04"))
		case 'S':
			b.WriteString(ts.Format("05"))
		case 'A':
			b.WriteString(ts.Format("Monday"))
		case 'a':
			b.WriteString(ts.Format("Mon"))
		case 'B':
			b.WriteString(ts.Format("January"))
		case 'b', 'h':
			b.WriteString(ts.Format("Jan"))
		case 'p':
			b.WriteString(ts.Format("PM"))
		case 'P':
			b.WriteString(ts.Format("pm"))
		case 'x', 'D':
			b.WriteString(ts.Format("01/02/06"))
		case 'X', 'T':
			b.WriteString(ts.Format("15:04:05"))
		case 'F':
			b.WriteString(ts.Format("2006-01-02"))
		case 'R':
			b.WriteString(ts.Format("15:04"))
		case 'r':
			b.WriteString(ts.Format("03:04:05 PM"))
		case 'c': // C locale: "%a %b %e %H:%M:%S %Y"
			b.WriteString(ts.Format("Mon Jan _2 15:04:05 2006"))
		case 'z':
			b.WriteString(ts.Format("-0700"))
		case 'Z':
			b.WriteString(ts.Format("MST"))
		case 's':
			b.WriteString(strconv.FormatInt(ts.Unix(), 10))
		case 'w': // weekday, 0 = Sunday
			b.WriteString(strconv.Itoa(int(ts.Weekday())))
		case 'u': // weekday, 1 = Monday .. 7 = Sunday
			b.WriteString(strconv.Itoa((int(ts.Weekday())+6)%7 + 1))
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '%':
			b.WriteByte('%')
		default:
			Errorf("invalid date format directive '%%%c'", format[i])
		}
	}
	return b.String()
}

// installOS registers the "os" library.
func installOS(e *Env) {
	lib := NewTable()
	reg := func(name string, fn Fn) { lib.Set(String(name), NewClosure(fn, "os."+name)) }

	reg("time", func(env *Env, args []Value) []Value {
		if Arg(args, 0).IsNil() {
			return []Value{Number(float64(time.Now().Unix()))}
		}
		v := Arg(args, 0)
		tb := v.Table()
		if tb == nil {
			Errorf("bad argument #1 to 'time' (table expected, got %s)", v.Type())
		}
		tv := TableVal(tb)
		field := func(name string, required bool) int {
			fv := Index(env, tv, String(name))
			if fv.IsNil() && !required {
				return 0
			}
			f, ok := ToNumber(fv)
			if !ok {
				Errorf("field %q missing in date table", name)
			}
			return int(f)
		}
		year, month, day := field("year", true), field("month", true), field("day", true)
		hour, min, sec := field("hour", false), field("min", false), field("sec", false)
		ts := time.Date(year, time.Month(month), day, hour, min, sec, 0, time.Local)
		return []Value{Number(float64(ts.Unix()))}
	})

	reg("clock", func(env *Env, args []Value) []Value {
		return []Value{Number(time.Since(osStart).Seconds())}
	})

	reg("date", func(env *Env, args []Value) []Value {
		format := "%c" // default: the C-locale ctime string ("Mon Jan _2 15:04:05 2006")
		if !Arg(args, 0).IsNil() {
			format = osStr("date", args, 0)
		}
		loc := time.Local
		if strings.HasPrefix(format, "!") { // "!" forces UTC
			loc = time.UTC
			format = format[1:]
		}
		ts := time.Now()
		if !Arg(args, 1).IsNil() {
			ts = time.Unix(osInt("date", args, 1), 0)
		}
		ts = ts.In(loc)
		if format == "*t" {
			return []Value{TableVal(osDateTable(ts))}
		}
		return []Value{String(osFormatDate(format, ts))}
	})

	reg("getenv", func(env *Env, args []Value) []Value {
		if v, ok := os.LookupEnv(osStr("getenv", args, 0)); ok && v != "" {
			return []Value{String(v)}
		}
		return []Value{Nil}
	})

	reg("difftime", func(env *Env, args []Value) []Value {
		return []Value{Number(osNum("difftime", args, 0) - osNum("difftime", args, 1))}
	})

	e.Set("os", TableVal(lib))
}
