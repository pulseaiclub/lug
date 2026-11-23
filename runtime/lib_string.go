package runtime

import (
	"strings"
)

func init() { installers = append(installers, installString) }

func installString(e *Env) {
	s := e.strlib
	reg := func(name string, fn Fn) { s.Set(String(name), NewClosure(fn, "string."+name)) }
	reg("len", strLen)
	reg("sub", strSub)
	reg("upper", strUpper)
	reg("lower", strLower)
	reg("rep", strRep)
	reg("reverse", strReverse)
	reg("byte", strByte)
	reg("char", strChar)
	reg("format", strFormat)
	reg("find", strFind)
	reg("match", strMatch)
	reg("gmatch", strGmatch)
	reg("gsub", strGsub)
}

// checkStringArg returns argument i (1-based) as a string, applying Lua's
// string coercion of numbers, or raises an error.
func checkStringArg(args []Value, i int, fname string) string {
	v := Arg(args, i-1)
	switch v.kind {
	case KindString:
		return v.str
	case KindNumber:
		return NumToStr(v.num)
	}
	Errorf("bad argument #%d to '%s' (string expected, got %s)", i, fname, v.Type())
	return ""
}

func numArg(args []Value, i int, fname string) float64 {
	v := Arg(args, i-1)
	if f, ok := ToNumber(v); ok {
		return f
	}
	Errorf("bad argument #%d to '%s' (number expected, got %s)", i, fname, v.Type())
	return 0
}

func strLen(env *Env, args []Value) []Value {
	return []Value{Int(int64(len(checkStringArg(args, 1, "len"))))}
}

func strSub(env *Env, args []Value) []Value {
	s := checkStringArg(args, 1, "sub")
	i, j := 1.0, -1.0
	if len(args) >= 2 && !args[1].IsNil() {
		i = numArg(args, 2, "sub")
	}
	if len(args) >= 3 && !args[2].IsNil() {
		j = numArg(args, 3, "sub")
	}
	l := len(s)
	ii, jj := int(i), int(j)
	if ii < 0 {
		ii = l + ii + 1
	}
	if jj < 0 {
		jj = l + jj + 1
	}
	if ii < 1 {
		ii = 1
	}
	if jj > l {
		jj = l
	}
	if ii > jj {
		return []Value{String("")}
	}
	return []Value{String(s[ii-1 : jj])}
}

func strUpper(env *Env, args []Value) []Value {
	return []Value{String(strings.ToUpper(checkStringArg(args, 1, "upper")))}
}

func strLower(env *Env, args []Value) []Value {
	return []Value{String(strings.ToLower(checkStringArg(args, 1, "lower")))}
}

func strRep(env *Env, args []Value) []Value {
	s := checkStringArg(args, 1, "rep")
	n := int64(numArg(args, 2, "rep"))
	sep := ""
	if len(args) >= 3 && !args[2].IsNil() {
		sep = checkStringArg(args, 3, "rep")
	}
	if n <= 0 {
		return []Value{String("")}
	}
	if n > 1e7 {
		ErrorString("resulting string too large")
	}
	return []Value{String(strings.Repeat(s+sep, int(n)-1) + s)}
}

func strReverse(env *Env, args []Value) []Value {
	s := checkStringArg(args, 1, "reverse")
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return []Value{String(string(b))}
}

func strByte(env *Env, args []Value) []Value {
	s := checkStringArg(args, 1, "byte")
	i, j := 1.0, 1.0
	if len(args) >= 2 && !args[1].IsNil() {
		i = numArg(args, 2, "byte")
	}
	if len(args) >= 3 && !args[2].IsNil() {
		j = numArg(args, 3, "byte")
	} else {
		j = i
	}
	l := len(s)
	ii, jj := int(i), int(j)
	if ii < 0 {
		ii = l + ii + 1
	}
	if jj < 0 {
		jj = l + jj + 1
	}
	if ii < 1 {
		ii = 1
	}
	if jj > l {
		jj = l
	}
	out := make([]Value, 0, 4)
	for k := ii; k <= jj; k++ {
		out = append(out, Number(float64(s[k-1])))
	}
	return out
}

func strChar(env *Env, args []Value) []Value {
	b := make([]byte, 0, len(args))
	for i := range args {
		n, ok := ToNumber(args[i])
		if !ok || n != float64(int64(n)) {
			Errorf("bad argument #%d to 'char' (number expected)", i+1)
		}
		b = append(b, byte(int64(n)))
	}
	return []Value{String(string(b))}
}

// ---------------------------------------------------------------------------
// Pattern matching (string.find / match / gmatch / gsub)
// ---------------------------------------------------------------------------

func patInit(args []Value, argi int, fname string, slen int) int {
	init := 1.0
	if len(args) >= argi && !args[argi-1].IsNil() {
		init = numArg(args, argi, fname)
	}
	i := int(init)
	if i < 0 {
		i = slen + i + 1
	}
	if i < 1 {
		i = 1
	}
	return i
}

// capsToValues converts 0-based capture bounds to Lua values (1-based
// positions for position captures).
func capsToValues(s string, caps [][2]int) []Value {
	out := make([]Value, len(caps))
	for i, c := range caps {
		if c[0] < 0 {
			out[i] = Int(int64(c[1] + 1))
		} else {
			out[i] = String(s[c[0]:c[1]])
		}
	}
	return out
}

func strFind(env *Env, args []Value) []Value {
	s := checkStringArg(args, 1, "find")
	pat := checkStringArg(args, 2, "find")
	init := patInit(args, 3, "find", len(s))
	plain := len(args) >= 4 && args[3].Truth()
	if init > len(s)+1 {
		return []Value{Nil}
	}
	if plain || !strings.ContainsAny(pat, "^$*+?.([%-") {
		idx := strings.Index(s[init-1:], pat)
		if idx < 0 {
			return []Value{Nil}
		}
		start := init - 1 + idx
		return []Value{Int(int64(start + 1)), Int(int64(start + len(pat)))}
	}
	start, end, caps, ok, err := patMatch(s, pat, init-1)
	if err != nil {
		Error(String(err.Error()))
	}
	if !ok {
		return []Value{Nil}
	}
	res := []Value{Int(int64(start + 1)), Int(int64(end))}
	return append(res, capsToValues(s, caps)...)
}

func strMatch(env *Env, args []Value) []Value {
	s := checkStringArg(args, 1, "match")
	pat := checkStringArg(args, 2, "match")
	init := patInit(args, 3, "match", len(s))
	if init > len(s)+1 {
		return nil
	}
	start, end, caps, ok, err := patMatch(s, pat, init-1)
	if err != nil {
		Error(String(err.Error()))
	}
	if !ok {
		return []Value{Nil} // Lua's string.match returns nil on no match
	}
	if len(caps) == 0 {
		return []Value{String(s[start:end])}
	}
	return capsToValues(s, caps)
}

func strGmatch(env *Env, args []Value) []Value {
	s := checkStringArg(args, 1, "gmatch")
	pat := checkStringArg(args, 2, "gmatch")
	pos := 0
	return []Value{NewClosure(func(env *Env, _ []Value) []Value {
		for pos <= len(s) {
			start, end, caps, ok, err := patMatch(s, pat, pos)
			if err != nil {
				Error(String(err.Error()))
			}
			if !ok {
				return nil
			}
			if end == start {
				pos = start + 1 // guarantee progress on empty matches
			} else {
				pos = end
			}
			if len(caps) == 0 {
				return []Value{String(s[start:end])}
			}
			return capsToValues(s, caps)
		}
		return nil
	}, "gmatch")}
}

func strGsub(env *Env, args []Value) []Value {
	s := checkStringArg(args, 1, "gsub")
	pat := checkStringArg(args, 2, "gsub")
	repl := Arg(args, 2)
	maxN := -1
	if len(args) >= 4 && !args[3].IsNil() {
		maxN = int(numArg(args, 4, "gsub"))
	}
	var sb strings.Builder
	count := 0
	pos := 0
	for pos <= len(s) {
		if maxN >= 0 && count >= maxN {
			break
		}
		start, end, caps, ok, err := patMatch(s, pat, pos)
		if err != nil {
			Error(String(err.Error()))
		}
		if !ok {
			break
		}
		sb.WriteString(s[pos:start])
		sb.WriteString(applyRepl(env, s, repl, start, end, caps))
		count++
		if end == start {
			if start < len(s) {
				sb.WriteByte(s[start])
			}
			pos = start + 1
		} else {
			pos = end
		}
	}
	sb.WriteString(s[pos:])
	return []Value{String(sb.String()), Int(int64(count))}
}

func applyRepl(env *Env, s string, repl Value, start, end int, caps [][2]int) string {
	whole := s[start:end]
	if len(caps) == 0 {
		return applyReplOne(env, repl, whole, []Value{String(whole)})
	}
	return applyReplOne(env, repl, whole, capsToValues(s, caps))
}

func applyReplOne(env *Env, repl Value, whole string, capVals []Value) string {
	switch repl.kind {
	case KindString:
		return expandRepl(repl.str, whole, capVals)
	case KindTable:
		return replResult(env, Index(env, repl, capVals[0]), whole)
	case KindFunction:
		return replResult(env, Call1(env, repl, capVals), whole)
	}
	Errorf("bad argument #3 to 'gsub' (string/function/table expected)")
	return ""
}

func replResult(env *Env, v Value, whole string) string {
	switch v.kind {
	case KindNil, KindBool:
		if v.kind == KindNil || !v.BoolVal() {
			return whole // nil/false keeps the original match
		}
		ErrorString("invalid replacement value (a boolean)")
	case KindString:
		return v.str
	case KindNumber:
		return NumToStr(v.num)
	default:
		Errorf("invalid replacement value (a %s)", v.Type())
	}
	return ""
}

// expandRepl expands a string replacement: %0 whole match, %1..%9 captures,
// %% literal percent.
func expandRepl(repl, whole string, caps []Value) string {
	var sb strings.Builder
	for i := 0; i < len(repl); i++ {
		c := repl[i]
		if c != '%' {
			sb.WriteByte(c)
			continue
		}
		i++
		if i >= len(repl) {
			ErrorString("invalid use of '%' in replacement string")
		}
		d := repl[i]
		switch {
		case d == '%':
			sb.WriteByte('%')
		case d == '0':
			sb.WriteString(whole)
		case d >= '1' && d <= '9':
			n := int(d - '0')
			if n > len(caps) {
				Errorf("invalid capture index %%%d in replacement string", n)
			}
			sb.WriteString(ToString(caps[n-1]))
		default:
			ErrorString("invalid use of '%' in replacement string")
		}
	}
	return sb.String()
}
