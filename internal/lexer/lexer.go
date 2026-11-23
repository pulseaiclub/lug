// Package lexer tokenizes Lua 5.4 source code.
//
// Newlines are plain whitespace (never tokens): expression parsing stays
// greedy across lines, which is exactly how Lua resolves the
// `a = b \n (c).d` ambiguity (it parses as `a = b(c).d`).
package lexer

import (
	"fmt"
	"strconv"
	"strings"
)

// Type identifies a token kind.
type Type int

// Token types.
const (
	EOF Type = iota
	NAME
	NUMBER
	STRING

	// Keywords.
	AND
	BREAK
	DO
	ELSE
	ELSEIF
	END
	FALSE
	FOR
	FUNCTION
	GOTO
	IF
	IN
	LOCAL
	NIL
	NOT
	OR
	REPEAT
	RETURN
	THEN
	TRUE
	UNTIL
	WHILE

	// Symbols.
	ADD     // +
	SUB     // -
	MUL     // *
	DIV     // /
	IDIV    // //
	MOD     // %
	POW     // ^
	BAND    // &
	BOR     // |
	BXOR    // ~
	SHL     // <<
	SHR     // >>
	EQ      // ==
	NE      // ~=
	LE      // <=
	GE      // >=
	LT      // <
	GT      // >
	LEN     // #
	ASSIGN  // =
	LPAREN  // (
	RPAREN  // )
	LBRACE  // {
	RBRACE  // }
	LBRACK  // [
	RBRACK  // ]
	SEMI    // ;
	COLON   // :
	COMMA   // ,
	DOT     // .
	CONCAT  // ..
	VARARG  // ...
	DBCOLON // ::
)

var keywords = map[string]Type{
	"and": AND, "break": BREAK, "do": DO, "else": ELSE, "elseif": ELSEIF,
	"end": END, "false": FALSE, "for": FOR, "function": FUNCTION, "goto": GOTO,
	"if": IF, "in": IN, "local": LOCAL, "nil": NIL, "not": NOT, "or": OR,
	"repeat": REPEAT, "return": RETURN, "then": THEN, "true": TRUE,
	"until": UNTIL, "while": WHILE,
}

var typeNames = map[Type]string{
	EOF: "<eof>", NAME: "<name>", NUMBER: "<number>", STRING: "<string>",
	AND: "and", BREAK: "break", DO: "do", ELSE: "else", ELSEIF: "elseif",
	END: "end", FALSE: "false", FOR: "for", FUNCTION: "function", GOTO: "goto",
	IF: "if", IN: "in", LOCAL: "local", NIL: "nil", NOT: "not", OR: "or",
	REPEAT: "repeat", RETURN: "return", THEN: "then", TRUE: "true",
	UNTIL: "until", WHILE: "while",
	ADD: "+", SUB: "-", MUL: "*", DIV: "/", IDIV: "//", MOD: "%", POW: "^",
	BAND: "&", BOR: "|", BXOR: "~", SHL: "<<", SHR: ">>", EQ: "==", NE: "~=",
	LE: "<=", GE: ">=", LT: "<", GT: ">", LEN: "#", ASSIGN: "=",
	LPAREN: "(", RPAREN: ")", LBRACE: "{", RBRACE: "}", LBRACK: "[",
	RBRACK: "]", SEMI: ";", COLON: ":", COMMA: ",", DOT: ".", CONCAT: "..",
	VARARG: "...", DBCOLON: "::",
}

// Name returns a printable name for the token type.
func (t Type) Name() string { return typeNames[t] }

// Token is a single lexeme.
type Token struct {
	Type Type
	Line int
	Col  int
	Str  string  // NAME text, decoded STRING value, raw NUMBER text
	Num  float64 // parsed NUMBER value
}

// Error is a lexical error with a position.
type Error struct {
	Line int
	Col  int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg) }

// Lexer scans Lua source one token at a time.
type Lexer struct {
	src  []byte
	pos  int
	line int
	col  int
}

// New creates a lexer over src.
func New(src []byte) *Lexer {
	return &Lexer{src: src, line: 1, col: 1}
}

func (l *Lexer) errf(format string, args ...any) {
	panic(&Error{Line: l.line, Col: l.col, Msg: fmt.Sprintf(format, args...)})
}

func (l *Lexer) peek() byte {
	if l.pos < len(l.src) {
		return l.src[l.pos]
	}
	return 0
}

func (l *Lexer) peekAt(n int) byte {
	if l.pos+n < len(l.src) {
		return l.src[l.pos+n]
	}
	return 0
}

func (l *Lexer) advance() byte {
	c := l.src[l.pos]
	l.pos++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

// Next returns the next token, skipping whitespace and comments.
func (l *Lexer) Next() Token {
	l.skipSpaceAndComments()
	line, col := l.line, l.col
	if l.pos >= len(l.src) {
		return Token{Type: EOF, Line: line, Col: col}
	}
	c := l.peek()
	switch {
	case isAlpha(c):
		return l.scanName(line, col)
	case c == '.' && isDigit(l.peekAt(1)):
		return l.scanNumber(line, col)
	case isDigit(c):
		return l.scanNumber(line, col)
	case c == '"' || c == '\'':
		return l.scanString(line, col, c)
	case c == '[':
		if l.longBracketLevel() >= 0 {
			return l.scanLongString(line, col)
		}
		l.advance()
		return Token{Type: LBRACK, Line: line, Col: col}
	}
	l.advance()
	two := string([]byte{c, l.peek()})
	switch two {
	case "//":
		l.advance()
		return Token{Type: IDIV, Line: line, Col: col}
	case "<<":
		l.advance()
		return Token{Type: SHL, Line: line, Col: col}
	case ">>":
		l.advance()
		return Token{Type: SHR, Line: line, Col: col}
	case "==":
		l.advance()
		return Token{Type: EQ, Line: line, Col: col}
	case "~=":
		l.advance()
		return Token{Type: NE, Line: line, Col: col}
	case "<=":
		l.advance()
		return Token{Type: LE, Line: line, Col: col}
	case ">=":
		l.advance()
		return Token{Type: GE, Line: line, Col: col}
	case "..":
		if l.peekAt(1) == '.' {
			l.advance()
			l.advance()
			return Token{Type: VARARG, Line: line, Col: col}
		}
		l.advance()
		return Token{Type: CONCAT, Line: line, Col: col}
	case "::":
		l.advance()
		return Token{Type: DBCOLON, Line: line, Col: col}
	}
	var t Type
	switch c {
	case '+':
		t = ADD
	case '-':
		t = SUB
	case '*':
		t = MUL
	case '/':
		t = DIV
	case '%':
		t = MOD
	case '^':
		t = POW
	case '&':
		t = BAND
	case '|':
		t = BOR
	case '~':
		t = BXOR
	case '<':
		t = LT
	case '>':
		t = GT
	case '#':
		t = LEN
	case '=':
		t = ASSIGN
	case '(':
		t = LPAREN
	case ')':
		t = RPAREN
	case '{':
		t = LBRACE
	case '}':
		t = RBRACE
	case ']':
		t = RBRACK
	case ';':
		t = SEMI
	case ':':
		t = COLON
	case ',':
		t = COMMA
	case '.':
		t = DOT
	default:
		l.errf("unexpected symbol near '%c'", c)
	}
	return Token{Type: t, Line: line, Col: col}
}

func (l *Lexer) skipSpaceAndComments() {
	for l.pos < len(l.src) {
		c := l.peek()
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\v' || c == '\f':
			l.advance()
		case c == '-' && l.peekAt(1) == '-':
			l.advance()
			l.advance()
			if l.peek() == '[' && l.longBracketLevel() >= 0 {
				l.skipLongBracket()
			} else {
				for l.pos < len(l.src) && l.peek() != '\n' {
					l.advance()
				}
			}
		default:
			return
		}
	}
}

// longBracketLevel checks whether the current position starts a long
// bracket "[[" or "[=...=[" and returns its level (the number of '='),
// or -1 when it does not. The opening bracket is NOT consumed.
func (l *Lexer) longBracketLevel() int {
	if l.peek() != '[' {
		return -1
	}
	i := 1
	for ; l.peekAt(i) == '='; i++ {
	}
	if l.peekAt(i) == '[' {
		return i - 1 // number of '=' between the brackets
	}
	return -1
}

func (l *Lexer) skipLongBracket() {
	level := l.longBracketLevel()
	l.advance() // '['
	for i := 0; i < level; i++ {
		l.advance() // '='s
	}
	l.advance() // '['
	l.skipFirstNewline()
	for l.pos < len(l.src) {
		if l.peek() == ']' && l.matchClose(level) {
			return
		}
		l.advance()
	}
	l.errf("unfinished long comment")
}

// matchClose consumes a closing long bracket of the given level when the
// current position starts one; otherwise leaves the position unchanged.
func (l *Lexer) matchClose(level int) bool {
	if l.peek() != ']' {
		return false
	}
	i := 1
	for ; i <= level; i++ {
		if l.peekAt(i) != '=' {
			return false
		}
	}
	if l.peekAt(level+1) != ']' {
		return false
	}
	for i := 0; i < level+2; i++ {
		l.advance()
	}
	return true
}

// skipFirstNewline skips a newline immediately following a long bracket
// opener, as Lua does.
func (l *Lexer) skipFirstNewline() {
	if l.peek() == '\r' {
		l.advance()
		if l.peek() == '\n' {
			l.advance()
		}
	} else if l.peek() == '\n' {
		l.advance()
	}
}

func (l *Lexer) scanName(line, col int) Token {
	start := l.pos
	for isAlnum(l.peek()) {
		l.advance()
	}
	text := string(l.src[start:l.pos])
	if t, ok := keywords[text]; ok {
		return Token{Type: t, Line: line, Col: col, Str: text}
	}
	return Token{Type: NAME, Line: line, Col: col, Str: text}
}

func (l *Lexer) scanNumber(line, col int) Token {
	start := l.pos
	if l.peek() == '0' && (l.peekAt(1) == 'x' || l.peekAt(1) == 'X') {
		l.advance()
		l.advance()
		for isHexDigit(l.peek()) {
			l.advance()
		}
		if l.peek() == '.' {
			l.advance()
			for isHexDigit(l.peek()) {
				l.advance()
			}
		}
		if l.peek() == 'p' || l.peek() == 'P' {
			l.advance()
			if l.peek() == '+' || l.peek() == '-' {
				l.advance()
			}
			if !isDigit(l.peek()) {
				l.errf("malformed number near '%s'", string(l.src[start:l.pos]))
			}
			for isDigit(l.peek()) {
				l.advance()
			}
		}
	} else {
		for isDigit(l.peek()) {
			l.advance()
		}
		// A '.' begins a fraction unless it is the start of '..' or '...'.
		if l.peek() == '.' && l.peekAt(1) != '.' {
			l.advance()
			for isDigit(l.peek()) {
				l.advance()
			}
		}
		if l.peek() == 'e' || l.peek() == 'E' {
			l.advance()
			if l.peek() == '+' || l.peek() == '-' {
				l.advance()
			}
			if !isDigit(l.peek()) {
				l.errf("malformed number near '%s'", string(l.src[start:l.pos]))
			}
			for isDigit(l.peek()) {
				l.advance()
			}
		}
	}
	text := string(l.src[start:l.pos])
	if isAlpha(l.peek()) {
		l.errf("malformed number near '%s%s'", text, string(l.src[l.pos:l.pos+1]))
	}
	num, ok := parseLuaNumber(text)
	if !ok {
		l.errf("malformed number near '%s'", text)
	}
	return Token{Type: NUMBER, Line: line, Col: col, Str: text, Num: num}
}

func (l *Lexer) scanString(line, col int, quote byte) Token {
	l.advance() // opening quote
	var sb strings.Builder
	for {
		if l.pos >= len(l.src) {
			l.errf("unfinished string")
		}
		c := l.peek()
		if c == '\n' {
			l.errf("unfinished string")
		}
		if c == quote {
			l.advance()
			break
		}
		if c == '\\' {
			l.advance()
			l.readEscape(&sb, quote)
			continue
		}
		sb.WriteByte(c)
		l.advance()
	}
	return Token{Type: STRING, Line: line, Col: col, Str: sb.String()}
}

func (l *Lexer) readEscape(sb *strings.Builder, quote byte) {
	if l.pos >= len(l.src) {
		l.errf("unfinished string")
	}
	c := l.advance()
	switch c {
	case 'a':
		sb.WriteByte(7)
	case 'b':
		sb.WriteByte(8)
	case 'f':
		sb.WriteByte(12)
	case 'n':
		sb.WriteByte(10)
	case 'r':
		sb.WriteByte(13)
	case 't':
		sb.WriteByte(9)
	case 'v':
		sb.WriteByte(11)
	case '\\':
		sb.WriteByte('\\')
	case '"':
		sb.WriteByte('"')
	case '\'':
		sb.WriteByte('\'')
	case '\n':
		sb.WriteByte(10)
	case '\r':
		if l.peek() == '\n' {
			l.advance()
		}
		sb.WriteByte(10)
	case 'x':
		h1, h2 := l.hexByte()
		sb.WriteByte(byte(h1<<4 | h2))
	case 'z':
		for l.pos < len(l.src) {
			c := l.peek()
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f' {
				l.advance()
			} else {
				break
			}
		}
	case 'u':
		if l.peek() != '{' {
			l.errf("missing '{' in \\u{xxxx}")
		}
		l.advance()
		r := 0
		n := 0
		for l.pos < len(l.src) && l.peek() != '}' {
			d := hexDigitVal(l.peek())
			if d < 0 {
				l.errf("UTF-8 value too large")
			}
			r = r<<4 | d
			if r > 0x7FFFFFFF {
				l.errf("UTF-8 value too large")
			}
			l.advance()
			n++
		}
		if n == 0 || l.peek() != '}' {
			l.errf("missing '}' in \\u{xxxx}")
		}
		l.advance()
		sb.WriteString(string(rune(r)))
	default:
		if isDigit(c) {
			v := int(c - '0')
			for i := 0; i < 2 && isDigit(l.peek()); i++ {
				v = v*10 + int(l.advance()-'0')
			}
			if v > 255 {
				l.errf("decimal escape too large")
			}
			sb.WriteByte(byte(v))
			return
		}
		l.errf("invalid escape sequence '\\%c'", c)
	}
}

func (l *Lexer) hexByte() (int, int) {
	h1 := hexDigitVal(l.peek())
	if h1 < 0 {
		l.errf("hexadecimal digit expected")
	}
	l.advance()
	h2 := hexDigitVal(l.peek())
	if h2 < 0 {
		l.errf("hexadecimal digit expected")
	}
	l.advance()
	return h1, h2
}

func (l *Lexer) scanLongString(line, col int) Token {
	level := l.longBracketLevel()
	l.advance() // '['
	for i := 0; i < level; i++ {
		l.advance()
	}
	l.advance() // '['
	l.skipFirstNewline()
	start := l.pos
	for {
		if l.pos >= len(l.src) {
			l.errf("unfinished long string")
		}
		if l.peek() == ']' && l.matchClose(level) {
			break
		}
		l.advance()
	}
	return Token{Type: STRING, Line: line, Col: col, Str: string(l.src[start : l.pos-level-2])}
}

// parseLuaNumber converts a Lua numeric literal to float64.
func parseLuaNumber(text string) (float64, bool) {
	if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
		s := text[2:]
		mant := s
		exp := 0
		if i := strings.IndexAny(s, "pP"); i >= 0 {
			e, err := strconv.Atoi(s[i+1:])
			if err != nil {
				return 0, false
			}
			exp = e
			mant = s[:i]
		}
		intPart, fracPart := mant, ""
		if i := strings.IndexByte(mant, '.'); i >= 0 {
			intPart, fracPart = mant[:i], mant[i+1:]
		}
		if intPart == "" && fracPart == "" {
			return 0, false
		}
		iv, err := strconv.ParseUint(intPart, 16, 64)
		if err != nil {
			return 0, false
		}
		f := float64(iv)
		scale := 1.0 / 16.0
		for i := 0; i < len(fracPart); i++ {
			d := hexDigitVal(fracPart[i])
			if d < 0 {
				return 0, false
			}
			f += float64(d) * scale
			scale /= 16
		}
		return f * pow2(exp), true
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func pow2(exp int) float64 {
	r := 1.0
	base := 2.0
	e := exp
	if e < 0 {
		base = 0.5
		e = -e
	}
	for e > 0 {
		if e&1 == 1 {
			r *= base
		}
		base *= base
		e >>= 1
	}
	return r
}

func isAlpha(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAlnum(c byte) bool { return isAlpha(c) || isDigit(c) }

func isHexDigit(c byte) bool { return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }

func hexDigitVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
