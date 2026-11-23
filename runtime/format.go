package runtime

import (
	"fmt"
	"strconv"
	"strings"
)

// strFormat implements string.format with C-printf-style directives:
// %[flags][width][.precision]conversion.
//
// args[0] is the format string; the remaining arguments are consumed in
// order. Numbers are formatted like C (Go's fmt verbs are C-compatible for
// e/E/f/F/g/G/a/A); %d requires an integer representation; %q produces a
// Lua-style quoted string.
func strFormat(env *Env, args []Value) []Value {
	if len(args) == 0 || args[0].kind != KindString {
		Errorf("bad argument #1 to 'format' (string expected, got %s)", Arg(args, 0).Type())
	}
	f := args[0].str
	argi := 1
	var sb strings.Builder
	i := 0
	for i < len(f) {
		c := f[i]
		if c != '%' {
			sb.WriteByte(c)
			i++
			continue
		}
		i++
		if i >= len(f) {
			ErrorString("invalid option '%' to 'format'")
		}
		var flags []byte
		for i < len(f) && strings.IndexByte("-+ #0", f[i]) >= 0 {
			flags = append(flags, f[i])
			i++
		}
		width := ""
		for i < len(f) && f[i] >= '0' && f[i] <= '9' {
			width += string(f[i])
			i++
		}
		prec := -1
		if i < len(f) && f[i] == '.' {
			i++
			prec = 0
			p := ""
			for i < len(f) && f[i] >= '0' && f[i] <= '9' {
				p += string(f[i])
				i++
			}
			if p != "" {
				prec, _ = strconv.Atoi(p)
			}
		}
		if i >= len(f) {
			ErrorString("invalid option '%' to 'format'")
		}
		conv := f[i]
		i++
		nextArg := func() Value {
			if argi >= len(args) {
				Errorf("bad argument #%d to 'format' (no value)", argi+1)
			}
			v := args[argi]
			argi++
			return v
		}
		flagStr := string(flags)
		switch conv {
		case '%':
			sb.WriteByte('%')
		case 'c':
			n := fmtInt(nextArg(), argi)
			sb.WriteByte(byte(n))
		case 'd', 'i', 'u':
			n := fmtInt(nextArg(), argi)
			sb.WriteString(fmt.Sprintf(fmtVerb(flagStr, width, -1, 'd'), n))
		case 'o', 'x', 'X':
			n := fmtInt(nextArg(), argi)
			sb.WriteString(fmt.Sprintf(fmtVerb(flagStr, width, prec, conv), uint64(n)))
		case 'e', 'E', 'f', 'F', 'g', 'G', 'a', 'A':
			v := nextArg()
			x, ok := ToNumber(v)
			if !ok {
				Errorf("bad argument #%d to 'format' (number expected, got %s)", argi, v.Type())
			}
			sb.WriteString(fmt.Sprintf(fmtVerb(flagStr, width, prec, conv), x))
		case 'q':
			v := nextArg()
			switch v.kind {
			case KindString:
				sb.WriteString(luaQuote(v.str))
			case KindNumber:
				sb.WriteString(luaQuote(NumToStr(v.num)))
			default:
				sb.WriteString(luaQuote(ToStringEnv(env, v)))
			}
		case 's':
			v := nextArg()
			s := ToStringEnv(env, v)
			if prec >= 0 && prec < len(s) {
				s = s[:prec]
			}
			// C ignores the '0' flag for %s.
			sb.WriteString(fmt.Sprintf(fmtVerb(strings.ReplaceAll(flagStr, "0", ""), width, -1, 's'), s))
		default:
			Errorf("invalid option '%%%c' to 'format'", conv)
		}
	}
	return []Value{String(sb.String())}
}

// fmtVerb builds a Go fmt verb from parsed C format pieces.
func fmtVerb(flags, width string, prec int, verb byte) string {
	var b strings.Builder
	b.WriteByte('%')
	b.WriteString(flags)
	if width != "" {
		b.WriteString(width)
	}
	if prec >= 0 {
		b.WriteByte('.')
		b.WriteString(strconv.Itoa(prec))
	}
	b.WriteByte(verb)
	return b.String()
}

// fmtInt converts a value to an int64 for %d/%c/%x, requiring an integer
// representation like Lua.
func fmtInt(v Value, argi int) int64 {
	if n, ok := toInt64(v); ok {
		return n
	}
	Errorf("bad argument #%d to 'format' (number has no integer representation)", argi)
	return 0
}

// luaQuote produces a Lua-style double-quoted string literal.
func luaQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 32 || c > 126 {
				fmt.Fprintf(&b, `\%03d`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
