package runtime

import "fmt"

// Lua 5.4 pattern matching, ported from lstrlib.c: the engine behind
// string.find / match / gmatch / gsub. Patterns operate on BYTES; nothing
// here looks at UTF-8 runes.
//
// The matcher is Lua's backtracking matcher over (pattern position, string
// position). Malformed patterns are detected lazily, exactly like Lua: the
// error is raised only when the matcher actually visits the bad item, so
// ("b"):find("a[") simply fails while ("a"):find("a[") raises an error.
// Errors travel as panic(*patError) — the analogue of Lua's luaL_error
// longjmp — and are recovered at the patMatch boundary. A quantifier that
// does not follow an item (e.g. "a**", "(a)*", a lone "+") is NOT an error
// in Lua: it is an ordinary literal byte, and this port keeps that behavior.

const (
	patEsc         = '%' // escape character
	patMaxCaptures = 32  // LUA_MAXCAPTURES
	patMaxDepth    = 200 // MAXCCALLS: recursion budget of match per attempt
)

// Capture length sentinels.
const (
	patCapUnfinished = -1 // '(' still open
	patCapPosition   = -2 // '()' position capture
)

// patError is a malformed-pattern error raised during matching. Messages
// follow Lua's wording, e.g. "malformed pattern (ends with '%')".
type patError struct{ msg string }

func (e *patError) Error() string { return e.msg }

func (ms *patState) fail(msg string) { panic(&patError{msg}) }

// patCapture is one open or closed capture.
type patCapture struct {
	init int // byte offset in the subject where the capture starts
	len  int // length in bytes, or patCapUnfinished / patCapPosition
}

// patState is MatchState in lstrlib.c.
type patState struct {
	s        string                     // subject
	p        string                     // pattern (anchor already stripped)
	srcEnd   int                        // len(s)
	level    int                        // captures on the stack (open + closed)
	depth    int                        // remaining recursion budget
	captures [patMaxCaptures]patCapture // captures[0:level]
}

// patMatch reports whether pat matches in s at or after init (0-based byte offset).
// On success it returns the whole-match bounds [start, end) (0-based, half-open)
// and the capture bounds (each also [start, end); position captures from "()"
// are represented as {-1, pos} where pos is the 0-based byte position).
// ok=false (err nil) when there is no match; err non-nil for malformed patterns.
func patMatch(s, pat string, init int) (start, end int, caps [][2]int, ok bool, err error) {
	if init < 0 {
		init = 0
	}
	if init > len(s) {
		return 0, 0, nil, false, nil // cannot match anything
	}
	ms := &patState{s: s, p: pat, srcEnd: len(s)}
	anchor := len(pat) > 0 && pat[0] == '^'
	if anchor {
		ms.p = pat[1:]
	}
	defer func() {
		if r := recover(); r != nil {
			pe, isPatErr := r.(*patError)
			if !isPatErr {
				panic(r)
			}
			start, end, caps, ok, err = 0, 0, nil, false, pe
		}
	}()
	for pos := init; ; pos++ {
		ms.level = 0
		ms.depth = patMaxDepth
		if e, found := ms.match(pos, 0); found {
			return pos, e, ms.captureRanges(), true, nil
		}
		if anchor || pos >= len(s) {
			break
		}
	}
	return 0, 0, nil, false, nil
}

// captureRanges converts the captures of a successful match into byte ranges
// [start, end); position captures become {-1, pos}. Like Lua's
// push_onecapture, a successful match that leaves a capture open is an error.
func (ms *patState) captureRanges() [][2]int {
	if ms.level == 0 {
		return nil
	}
	caps := make([][2]int, ms.level)
	for i, c := range ms.captures[:ms.level] {
		switch c.len {
		case patCapUnfinished:
			ms.fail("unfinished capture")
		case patCapPosition:
			caps[i] = [2]int{-1, c.init}
		default:
			caps[i] = [2]int{c.init, c.init + c.len}
		}
	}
	return caps
}

// match tries to match ms.p[p:] against ms.s at s and returns the end
// position of the match (match in lstrlib.c). The label init is Lua's "init:"
// label: the tail calls written there as "goto init" cost no recursion depth,
// which is what makes the depth budget behave exactly like Lua's.
func (ms *patState) match(s, p int) (int, bool) {
	ms.depth--
	if ms.depth < 0 {
		ms.fail("pattern too complex")
	}
	defer func() { ms.depth++ }()

init:
	if p == len(ms.p) { // end of pattern: match succeeded
		return s, true
	}
	switch ms.p[p] {
	case '(': // start capture
		if p+1 < len(ms.p) && ms.p[p+1] == ')' { // position capture
			return ms.startCapture(s, p+2, patCapPosition)
		}
		return ms.startCapture(s, p+1, patCapUnfinished)

	case ')': // end capture
		return ms.endCapture(s, p+1)

	case '$':
		if p+1 == len(ms.p) { // is it the last pattern char?
			if s == ms.srcEnd {
				return s, true
			}
			return 0, false
		}
		// otherwise '$' is an ordinary literal item (handled below)

	case '%':
		if p+1 < len(ms.p) {
			switch ms.p[p+1] {
			case 'b': // balanced match
				res, found := ms.matchBalance(s, p+2)
				if !found {
					return 0, false
				}
				s, p = res, p+4
				goto init
			case 'f': // frontier
				if p+2 >= len(ms.p) || ms.p[p+2] != '[' {
					ms.fail("malformed pattern (missing '[' after '%f')")
				}
				ep := ms.classEnd(p + 2)
				var prev, cur byte // virtual '\0' before the string and after its end
				if s > 0 {
					prev = ms.s[s-1]
				}
				if s < ms.srcEnd {
					cur = ms.s[s]
				}
				if !ms.matchBracketClass(prev, p+2, ep-1) &&
					ms.matchBracketClass(cur, p+2, ep-1) {
					p = ep
					goto init
				}
				return 0, false
			case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9': // back reference
				res, found := ms.matchCapture(s, int(ms.p[p+1])-'1')
				if !found {
					return 0, false
				}
				s, p = res, p+2
				goto init
			}
		}
		// Any other %X is an ordinary single-character item (handled below);
		// a dangling '%' is reported by classEnd.
	}

	// default: a single-character item with an optional suffix
	ep := ms.classEnd(p) // points to the suffix, if any
	m := ms.singleMatch(s, p, ep)
	if ep < len(ms.p) {
		switch ms.p[ep] {
		case '?': // optional
			if m {
				if res, found := ms.match(s+1, ep+1); found {
					return res, true
				}
			}
			p = ep + 1
			goto init
		case '+': // 1 or more repetitions
			if !m {
				return 0, false
			}
			s++ // 1 match already counted
			fallthrough
		case '*': // 0 or more repetitions, greedy
			return ms.maxExpand(s, p, ep)
		case '-': // 0 or more repetitions, lazy
			return ms.minExpand(s, p, ep)
		}
	}
	// no suffix
	if !m {
		return 0, false
	}
	s++
	p = ep
	goto init
}

// classEnd returns the pattern index just past the single-character item
// starting at p (classend in lstrlib.c). It validates '%' escapes and sets:
// inside a set a ']' right after '[' or '[^' is a literal member, and every
// "%X" pair is skipped as a unit, so a '%' immediately before ']' leaves the
// set unterminated.
func (ms *patState) classEnd(p int) int {
	switch ms.p[p] {
	case patEsc:
		p++
		if p == len(ms.p) {
			ms.fail("malformed pattern (ends with '%')")
		}
	case '[':
		p++ // skip '['
		if p < len(ms.p) && ms.p[p] == '^' {
			p++ // skip '^'
		}
		for { // look for ']'
			if p >= len(ms.p) {
				ms.fail("malformed pattern (missing ']')")
			}
			c := ms.p[p]
			p++
			if c == patEsc && p < len(ms.p) {
				p++ // skip the escaped character
			}
			if p < len(ms.p) && ms.p[p] == ']' {
				break
			}
		}
	}
	return p + 1
}

// singleMatch reports whether the byte at s matches the single-character item
// ms.p[p:ep]. It never matches at the end of the subject (singlematch in
// lstrlib.c).
func (ms *patState) singleMatch(s, p, ep int) bool {
	if s >= ms.srcEnd {
		return false
	}
	c := ms.s[s]
	switch ms.p[p] {
	case '.':
		return true // matches any byte
	case patEsc:
		return patMatchClass(c, ms.p[p+1])
	case '[':
		return ms.matchBracketClass(c, p, ep-1)
	}
	return ms.p[p] == c
}

// matchBracketClass reports whether c belongs to the set ms.p[p:ec], where
// ms.p[p] == '[' and ms.p[ec] == ']' (matchbracketclass in lstrlib.c).
func (ms *patState) matchBracketClass(c byte, p, ec int) bool {
	sig := true
	p++ // skip '['
	if ms.p[p] == '^' {
		sig = false // negated set
		p++         // skip '^'
	}
	for p < ec {
		switch {
		case ms.p[p] == patEsc: // %<class> or literal %X
			p++
			if patMatchClass(c, ms.p[p]) {
				return sig
			}
		case ms.p[p+1] == '-' && p+2 < ec: // range x-y ("-y" would run past ']')
			p += 2
			if ms.p[p-2] <= c && c <= ms.p[p] {
				return sig
			}
		case ms.p[p] == c:
			return sig
		}
		p++
	}
	return !sig
}

// patMatchClass reports whether byte c matches the class %cl (match_class in
// lstrlib.c). A lowercase cl is the class and the uppercase letter is its
// complement; any other cl (digit, punctuation, ...) denotes the literal
// byte cl. Classes are ASCII only, exactly like Lua's C-locale ctype tables:
// bytes >= 0x80 belong to no lowercase class.
func patMatchClass(c, cl byte) bool {
	var res bool
	switch patLower(cl) {
	case 'a':
		res = patIsAlpha(c)
	case 'c':
		res = patIsCntrl(c)
	case 'd':
		res = patIsDigit(c)
	case 'g':
		res = patIsGraph(c)
	case 'l':
		res = patIsLower(c)
	case 'p':
		res = patIsPunct(c)
	case 's':
		res = patIsSpace(c)
	case 'u':
		res = patIsUpper(c)
	case 'w':
		res = patIsAlpha(c) || patIsDigit(c)
	case 'x':
		res = patIsXdigit(c)
	case 'z': // deprecated since 5.2: the zero byte
		res = c == 0
	default:
		return cl == c
	}
	if 'A' <= cl && cl <= 'Z' {
		return !res
	}
	return res
}

func patLower(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// ASCII (C locale) byte classification used by the %a %c %d %g %l %p %s
// %u %w %x classes.
func patIsAlpha(c byte) bool  { return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' }
func patIsCntrl(c byte) bool  { return c < 0x20 || c == 0x7f }
func patIsDigit(c byte) bool  { return '0' <= c && c <= '9' }
func patIsGraph(c byte) bool  { return 0x21 <= c && c <= 0x7e }
func patIsLower(c byte) bool  { return 'a' <= c && c <= 'z' }
func patIsUpper(c byte) bool  { return 'A' <= c && c <= 'Z' }
func patIsXdigit(c byte) bool { return patIsDigit(c) || 'A' <= c && c <= 'F' || 'a' <= c && c <= 'f' }
func patIsSpace(c byte) bool  { return c == ' ' || '\t' <= c && c <= '\r' }
func patIsPunct(c byte) bool  { return patIsGraph(c) && !patIsAlpha(c) && !patIsDigit(c) }

// matchBalance matches a string whose opening byte is ms.p[p] and closing
// byte ms.p[p+1] (matchbalance in lstrlib.c): the string starts with the
// opener and nested openers/closers cancel out. The empty string is not
// balanced, and a subject that ends out of balance fails.
func (ms *patState) matchBalance(s, p int) (int, bool) {
	if p+1 >= len(ms.p) {
		ms.fail("malformed pattern (missing arguments to '%b')")
	}
	if s >= ms.srcEnd || ms.s[s] != ms.p[p] {
		return 0, false
	}
	b, e := ms.p[p], ms.p[p+1]
	cont := 1
	s++
	for s < ms.srcEnd {
		switch ms.s[s] {
		case e:
			cont--
			if cont == 0 {
				return s + 1, true
			}
		case b:
			cont++
		}
		s++
	}
	return 0, false // string ends out of balance
}

// maxExpand matches a greedy quantifier (* or +): it counts the longest run
// of the item at s, then tries the rest of the pattern after each shorter
// run, longest first (max_expand in lstrlib.c).
func (ms *patState) maxExpand(s, p, ep int) (int, bool) {
	i := 0 // maximum number of repetitions
	for ms.singleMatch(s+i, p, ep) {
		i++
	}
	for i >= 0 {
		if res, found := ms.match(s+i, ep+1); found {
			return res, true
		}
		i-- // else didn't match; try one fewer repetition
	}
	return 0, false
}

// minExpand matches a lazy quantifier (-): it tries the rest of the pattern
// first and extends the run by one item only on failure (min_expand in
// lstrlib.c).
func (ms *patState) minExpand(s, p, ep int) (int, bool) {
	for {
		if res, found := ms.match(s, ep+1); found {
			return res, true
		}
		if !ms.singleMatch(s, p, ep) {
			return 0, false
		}
		s++ // try with one more repetition
	}
}

// startCapture pushes a capture starting at s and continues matching at p
// (start_capture in lstrlib.c). If the continuation fails, the capture is
// undone so that backtracking sees the capture stack of the enclosing path.
func (ms *patState) startCapture(s, p, what int) (int, bool) {
	if ms.level >= patMaxCaptures {
		ms.fail("too many captures")
	}
	ms.captures[ms.level] = patCapture{init: s, len: what}
	ms.level++
	res, found := ms.match(s, p)
	if !found {
		ms.level-- // undo capture
	}
	return res, found
}

// endCapture closes the innermost open capture at s and continues matching
// at p (end_capture in lstrlib.c), reopening it if the continuation fails.
func (ms *patState) endCapture(s, p int) (int, bool) {
	l := ms.captureToClose()
	ms.captures[l].len = s - ms.captures[l].init // close capture
	res, found := ms.match(s, p)
	if !found {
		ms.captures[l].len = patCapUnfinished // undo capture
	}
	return res, found
}

// captureToClose returns the index of the innermost open capture.
func (ms *patState) captureToClose() int {
	for top := ms.level - 1; top >= 0; top-- {
		if ms.captures[top].len == patCapUnfinished {
			return top
		}
	}
	ms.fail("invalid pattern capture")
	return 0 // not reached
}

// matchCapture matches at s the text of capture number l (0-based) and
// returns the position after it (match_capture in lstrlib.c). Referencing a
// capture that does not exist or is still open is an error; a position
// capture has no text and never matches.
func (ms *patState) matchCapture(s, l int) (int, bool) {
	if l < 0 || l >= ms.level || ms.captures[l].len == patCapUnfinished {
		ms.fail(fmt.Sprintf("invalid capture index %%%d", l+1))
	}
	c := ms.captures[l]
	if c.len < 0 {
		return 0, false
	}
	if ms.srcEnd-s >= c.len && ms.s[c.init:c.init+c.len] == ms.s[s:s+c.len] {
		return s + c.len, true
	}
	return 0, false
}
