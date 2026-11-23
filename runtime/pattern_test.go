package runtime

import (
	"strings"
	"testing"
)

// patCapsEqual compares capture lists.
func patCapsEqual(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPatMatch(t *testing.T) {
	// Columns: name, s, pat, want start, want end, want caps, want ok,
	// want error text (non-empty: an error containing this text is required).
	// All positions are 0-based, half-open; position captures are {-1, pos}.
	tests := []struct {
		name    string
		s, pat  string
		start   int
		end     int
		caps    [][2]int
		ok      bool
		wantErr string
	}{
		// --- plain literals and '.'
		{"literal", "hello world", "wor", 6, 9, nil, true, ""},
		{"literal missing", "hello", "xyz", 0, 0, nil, false, ""},
		{"dot", "abc", ".", 0, 1, nil, true, ""},
		{"dot matches any byte", "\x00\n\xff", "...", 0, 3, nil, true, ""},
		{"dot stops at end", "abc", "....", 0, 0, nil, false, ""},
		{"greedy .* backtracks", "anything", ".*n", 0, 7, nil, true, ""},
		{"greedy .* then .", "anything", ".*n.", 0, 8, nil, true, ""},
		{"empty pattern", "abc", "", 0, 0, nil, true, ""},
		{"empty pattern empty subject", "", "", 0, 0, nil, true, ""},

		// --- character classes and their complements (ASCII only)
		{"%d", "x=7, y=2", "%d", 2, 3, nil, true, ""},
		{"%d+", "abc 1234 def", "%d+", 4, 8, nil, true, ""},
		{"%a+", "  x=7  ", "%a+", 2, 3, nil, true, ""},
		{"%s", "a b", "%s", 1, 2, nil, true, ""},
		{"%u", "aBc", "%u", 1, 2, nil, true, ""},
		{"%l", "Ab", "%l", 1, 2, nil, true, ""},
		{"%w", "-a_", "%w", 1, 2, nil, true, ""},
		{"%p", "a.b", "%p", 1, 2, nil, true, ""},
		{"%x", "g0f", "%x", 1, 2, nil, true, ""},
		{"%c", "a\nb", "%c", 1, 2, nil, true, ""},
		{"%g", " a", "%g", 1, 2, nil, true, ""},
		{"%z", "a\x00b", "%z", 1, 2, nil, true, ""},
		{"%D", "a1", "%D", 0, 1, nil, true, ""},
		{"%A", "a1b", "%A", 1, 2, nil, true, ""},
		{"%S", "a b", "%S", 0, 1, nil, true, ""},
		{"%U", "aB", "%U", 0, 1, nil, true, ""},
		{"%L", "aB", "%L", 1, 2, nil, true, ""},
		{"%W", "a-b", "%W", 1, 2, nil, true, ""},
		{"%P", "ab", "%P", 0, 1, nil, true, ""},
		{"%X", "g", "%X", 0, 1, nil, true, ""},
		{"%C", "a", "%C", 0, 1, nil, true, ""},
		{"%G", " ", "%G", 0, 1, nil, true, ""},
		{"%a is ASCII only", "é", "%a", 0, 0, nil, false, ""},
		{"%A matches non-ASCII bytes", "é", "%A", 0, 1, nil, true, ""},

		// --- sets
		{"set with class", "hello, world", "[%a,]+", 0, 6, nil, true, ""},
		{"set range", "abc123", "[a-c]+", 0, 3, nil, true, ""},
		{"negated set", "hello, world", "[^%s]+", 0, 6, nil, true, ""},
		{"literal ] first in set", "x]y", "[]]", 1, 2, nil, true, ""},
		{"literal ] in negated set", "a]b", "[^]]+", 0, 1, nil, true, ""},
		{"set of classes", "x9", "[%a%d]+", 0, 2, nil, true, ""},
		{"%- is literal dash in set", "a-b", "[a%-b]+", 0, 3, nil, true, ""},
		{"dash first is literal", "a-b", "[-ab]+", 0, 3, nil, true, ""},
		{"no range without end", "a-b", "[a-]+", 0, 2, nil, true, ""},
		{"digit range", "x9y", "[0-9]", 1, 2, nil, true, ""},
		{"negated set with range", "abc123", "[^a-c]+", 3, 6, nil, true, ""},
		{"escaped char in set", "a.b", "[%.]+", 1, 2, nil, true, ""},
		{"literal % in set", "a%b", "[%%]+", 1, 2, nil, true, ""},

		// --- quantifiers
		{"* greedy", "aaa", "a*", 0, 3, nil, true, ""},
		{"* empty match", "xxx", "a*", 0, 0, nil, true, ""},
		{"+ one or more", "baaa", "a+", 1, 4, nil, true, ""},
		{"+ needs one", "bbb", "a+", 0, 0, nil, false, ""},
		{"+ on empty subject", "", "a+", 0, 0, nil, false, ""},
		{"+ on dot", "aaab", ".+", 0, 4, nil, true, ""},
		{"- lazy empty", "aaa", "a-", 0, 0, nil, true, ""},
		{"greedy .*", "<a><b>", "<.*>", 0, 6, nil, true, ""},
		{"lazy .-", "<a><b>", "<.->", 0, 3, nil, true, ""},
		{"lazy .-b", "abcb", ".-b", 0, 2, nil, true, ""},
		{"greedy .*b", "abcb", ".*b", 0, 4, nil, true, ""},
		{"a-b", "xxabxb", "a-b", 2, 4, nil, true, ""},
		{"a*b", "xxabxb", "a*b", 2, 4, nil, true, ""},
		{"? present", "abc", "ab?c", 0, 3, nil, true, ""},
		{"? absent", "ac", "ab?c", 0, 2, nil, true, ""},
		{"? empty", "bc", "a?", 0, 0, nil, true, ""},
		{"? backtracks", "ab", "a?a", 0, 1, nil, true, ""},
		{"set ? backtracks", "abc", "[ab]?c", 1, 3, nil, true, ""},
		{"? then anchor", "a", "a?b?$", 0, 1, nil, true, ""},
		{".* on empty subject", "", ".*", 0, 0, nil, true, ""},
		{".- to end", "abc", ".-$", 0, 3, nil, true, ""},
		{".* to end", "abc", ".*$", 0, 3, nil, true, ""},
		{"quantifier on class", "  word  ", "%s*(%a+)", 0, 6, [][2]int{{2, 6}}, true, ""},
		{"quantifier on set", "abc123def", "[%a%d]+", 0, 9, nil, true, ""},
		{"escaped ^ with quantifier", "a^^b", "a%^+b", 0, 4, nil, true, ""},
		// In Lua a quantifier that does not follow an item is a literal byte:
		// "a**" is (a)* followed by '*', not an error.
		{"a** is a* then literal *", "aaa*", "a**", 0, 4, nil, true, ""},
		{"a** requires the literal *", "aaa", "a**", 0, 0, nil, false, ""},
		{"leading quantifier is a literal", "a+b", "+", 1, 2, nil, true, ""},

		// --- anchors
		{"^ at start", "hello", "^h", 0, 1, nil, true, ""},
		{"^ fails mid-string", "hello", "^e", 0, 0, nil, false, ""},
		{"^ only first occurrence", "abcabc", "^abc", 0, 3, nil, true, ""},
		{"$ at end", "hello", "o$", 4, 5, nil, true, ""},
		{"$ fails before end", "hello", "l$", 0, 0, nil, false, ""},
		{"l.*$", "hello", "l.*$", 2, 5, nil, true, ""},
		{"^hello$", "hello", "^hello$", 0, 5, nil, true, ""},
		{"^hello$ fails", "hellox", "^hello$", 0, 0, nil, false, ""},
		{"^ alone", "abc", "^", 0, 0, nil, true, ""},
		{"$ alone", "abc", "$", 3, 3, nil, true, ""},
		{"^$", "", "^$", 0, 0, nil, true, ""},
		{"^$ on nonempty", "a", "^$", 0, 0, nil, false, ""},
		{"$ not last is literal", "a$b", "a$b", 0, 3, nil, true, ""},
		{"escaped $", "a$b", "%$", 1, 2, nil, true, ""},
		{"^ not first is literal", "a^b", "a^b", 0, 3, nil, true, ""},

		// --- escaped magic characters
		{"%.", "a.b", "a%.b", 0, 3, nil, true, ""},
		{"%. is strict", "axb", "a%.b", 0, 0, nil, false, ""},
		{"%%", "a%b", "a%%b", 0, 3, nil, true, ""},
		{"%-", "a-b", "a%-b", 0, 3, nil, true, ""},
		{"%+", "a+b", "a%+b", 0, 3, nil, true, ""},
		{"%(", "a(b", "a%(b", 0, 3, nil, true, ""},
		{"%[", "a[b", "a%[b", 0, 3, nil, true, ""},
		{"%)", ")", "%)", 0, 1, nil, true, ""},

		// --- captures
		{"two captures", "key=value", "(%w+)=(%w+)", 0, 9, [][2]int{{0, 3}, {4, 9}}, true, ""},
		{"one capture", "hello world", "(o w)", 4, 7, [][2]int{{4, 7}}, true, ""},
		{"capture of greedy +", "hello world", "(%a+)", 0, 5, [][2]int{{0, 5}}, true, ""},
		{"empty capture", "abc", "(b*)", 0, 0, [][2]int{{0, 0}}, true, ""},
		{"position captures", "hello world", "()world()", 6, 11, [][2]int{{-1, 6}, {-1, 11}}, true, ""},
		{"position captures inner", "hello", "()ll()", 2, 4, [][2]int{{-1, 2}, {-1, 4}}, true, ""},
		{"position capture alone", "abc", "()", 0, 0, [][2]int{{-1, 0}}, true, ""},
		{"nested captures", "key = value", "((%a+)) = (%a+)", 0, 11, [][2]int{{0, 3}, {0, 3}, {6, 11}}, true, ""},
		{"sequential captures", "abc", "(a)(b)(c)", 0, 3, [][2]int{{0, 1}, {1, 2}, {2, 3}}, true, ""},
		{"two captures with +", "x=12, y=34", "(%w+)=(%d+)", 0, 4, [][2]int{{0, 1}, {2, 4}}, true, ""},
		{"backreference", "hello hello", "(%a+) %1", 0, 11, [][2]int{{0, 5}}, true, ""},
		{"backreference fails", "hello world", "(%a+) %1", 0, 0, nil, false, ""},
		{"backreference forces backtrack", "aaa", "(a*)%1", 0, 2, [][2]int{{0, 1}}, true, ""},
		{"backreference to position capture never matches", "ab", "()%1", 0, 0, nil, false, ""},
		{"backtracking reopens closed capture", "abc de", "(%a+)x", 0, 0, nil, false, ""},
		{"backtracking across positions undoes captures", "xaab", "((a))b", 2, 4, [][2]int{{2, 3}, {2, 3}}, true, ""},
		{"two words", "hello world", "(%a+) (%a+)", 0, 11, [][2]int{{0, 5}, {6, 11}}, true, ""},
		{"lazy captures", "aab", "(a-)(b*)", 0, 0, [][2]int{{0, 0}, {0, 0}}, true, ""},
		{"greedy captures then literal", "aab", "(a*)(a*)b", 0, 3, [][2]int{{0, 2}, {2, 2}}, true, ""},
		{"captures after backtrack", "aab", "(a*)(a*)a", 0, 2, [][2]int{{0, 1}, {1, 1}}, true, ""},

		// --- %b balanced match
		{"%b()", "(a(b)c)", "%b()", 0, 7, nil, true, ""},
		{"%b() mid string", "x (foo [bar]) y", "%b()", 2, 13, nil, true, ""},
		{"%b() unbalanced open", "(ab", "%b()", 0, 0, nil, false, ""},
		{"%b() unbalanced close", "ab)", "%b()", 0, 0, nil, false, ""},
		{"%b needs the open byte", "abc", "%b()", 0, 0, nil, false, ""},
		{"%b same bytes", "xxx", "%bxx", 0, 2, nil, true, ""},
		{"%b{}", "{a{b}c}", "%b{}", 0, 7, nil, true, ""},
		{"%b empty pair", "()", "%b()", 0, 2, nil, true, ""},

		// --- %f frontier
		{"%f[%a] at start", "THE (quick) fox", "%f[%a]%a+", 0, 3, nil, true, ""},
		{"%f[%l] skips upper case", "THE (quick) fox", "%f[%l]%a+", 5, 10, nil, true, ""},
		{"%f[%s] finds space", "hello world", "%f[%s]", 5, 5, nil, true, ""},
		{"%f[%s] none", "hello", "%f[%s]", 0, 0, nil, false, ""},
		{"%f position 0 uses virtual NUL", " the end", "%f[%s]", 0, 0, nil, true, ""},
		{"%f end uses virtual NUL", "hello", "%f[%z]", 5, 5, nil, true, ""},
		{"%f negated set", "abc", "%f[^%a]", 3, 3, nil, true, ""},
		{"%f[%d]", "x=1, y=2", "%f[%d]%d", 2, 3, nil, true, ""},
		{"%f[%d] then %d+", "abc123def", "%f[%d]%d+", 3, 6, nil, true, ""},
		{"%f with literal ]", "a]b", "%f[]]", 1, 1, nil, true, ""},
		{"%f on empty subject", "", "%f[%z]", 0, 0, nil, false, ""},

		// --- malformed patterns (errors are raised lazily, like Lua)
		{"malformed set", "a", "a[", 0, 0, nil, false, "malformed pattern (missing ']')"},
		{"malformed set at start", "x[", "[", 0, 0, nil, false, "malformed pattern (missing ']')"},
		{"unterminated set with escape", "a", "[a%", 0, 0, nil, false, "malformed pattern (missing ']')"},
		{"dangling % inside set", "a", "[%]", 0, 0, nil, false, "malformed pattern (missing ']')"},
		{"empty set", "x", "[]", 0, 0, nil, false, "malformed pattern (missing ']')"},
		{"trailing %", "a", "a%", 0, 0, nil, false, "malformed pattern (ends with '%')"},
		{"only %", "%", "%", 0, 0, nil, false, "malformed pattern (ends with '%')"},
		{"invalid capture index", "a", "%1", 0, 0, nil, false, "invalid capture index %1"},
		{"capture index 0", "a", "%0", 0, 0, nil, false, "invalid capture index %0"},
		{"capture index beyond", "a", "(a)%2", 0, 0, nil, false, "invalid capture index %2"},
		{"backreference to open capture", "a", "((a)%1)", 0, 0, nil, false, "invalid capture index %1"},
		{"unmatched )", "a", ")", 0, 0, nil, false, "invalid pattern capture"},
		{"unfinished capture", "a", "(a", 0, 0, nil, false, "unfinished capture"},
		{"%b missing args", "a", "%b", 0, 0, nil, false, "malformed pattern (missing arguments to '%b')"},
		{"%b one arg", "a", "%b(", 0, 0, nil, false, "malformed pattern (missing arguments to '%b')"},
		{"%f without set", "a", "%f", 0, 0, nil, false, "malformed pattern (missing '[' after '%f')"},
		{"%f with wrong set", "a", "%fa", 0, 0, nil, false, "malformed pattern (missing '[' after '%f')"},
		{"too many captures", "x", strings.Repeat("()", 33), 0, 0, nil, false, "too many captures"},
		{"pattern too complex", strings.Repeat("a", 300), strings.Repeat("a?", 250), 0, 0, nil, false, "pattern too complex"},
		{"deep but within budget", strings.Repeat("a", 300), strings.Repeat("a?", 150), 0, 150, nil, true, ""},
		{"bad set not reached is no error", "b", "a[", 0, 0, nil, false, ""},
		{"bad capture index not reached is no error", "b", "a%1", 0, 0, nil, false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end, caps, ok, err := patMatch(tc.s, tc.pat, 0)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("patMatch(%q, %q): error = %v, want one containing %q", tc.s, tc.pat, err, tc.wantErr)
				}
				if ok {
					t.Fatalf("patMatch(%q, %q): ok = true together with error %v", tc.s, tc.pat, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("patMatch(%q, %q): unexpected error: %v", tc.s, tc.pat, err)
			}
			if ok != tc.ok || start != tc.start || end != tc.end || !patCapsEqual(caps, tc.caps) {
				t.Fatalf("patMatch(%q, %q) = (%d, %d, %v, %v), want (%d, %d, %v, %v)",
					tc.s, tc.pat, start, end, caps, ok, tc.start, tc.end, tc.caps, tc.ok)
			}
		})
	}

	t.Run("LUA_MAXCAPTURES position captures", func(t *testing.T) {
		start, end, caps, ok, err := patMatch("x", strings.Repeat("()", 32), 0)
		if err != nil || !ok || start != 0 || end != 0 || len(caps) != 32 {
			t.Fatalf("patMatch(32 x ()) = (%d, %d, %d caps, %v, %v), want (0, 0, 32 caps, true, nil)",
				start, end, len(caps), ok, err)
		}
		for i, c := range caps {
			if c != [2]int{-1, 0} {
				t.Fatalf("capture %d = %v, want {-1, 0}", i, c)
			}
		}
	})
}

func TestPatMatchEdge(t *testing.T) {
	// Columns: name, s, pat, init, want start, want end, want caps, want ok,
	// want error text.
	tests := []struct {
		name    string
		s, pat  string
		init    int
		start   int
		end     int
		caps    [][2]int
		ok      bool
		wantErr string
	}{
		{"init < 0 clamps to 0", "hello", "l", -5, 2, 3, nil, true, ""},
		{"init < 0 empty pattern", "hello", "", -1, 0, 0, nil, true, ""},
		{"init > len never matches", "hello", "", 6, 0, 0, nil, false, ""},
		{"init > len with .*", "hello", ".*", 100, 0, 0, nil, false, ""},
		{"init > len anchored", "hello", "^.*", 6, 0, 0, nil, false, ""},
		{"init == len empty pattern", "hello", "", 5, 5, 5, nil, true, ""},
		{"init == len .*", "hello", ".*", 5, 5, 5, nil, true, ""},
		{"init == len $", "hello", "$", 5, 5, 5, nil, true, ""},
		{"init == len non-empty pattern", "hello", "o$", 5, 0, 0, nil, false, ""},
		{"init skips earlier match", "abcabc", "abc", 1, 3, 6, nil, true, ""},
		{"init skips byte", "hello world", "o", 5, 7, 8, nil, true, ""},
		{"init with capture", "hello world", "(%a+)", 6, 6, 11, [][2]int{{6, 11}}, true, ""},
		{"anchored at init", "abcabc", "^abc", 3, 3, 6, nil, true, ""},
		{"anchored fails mid-word", "abcabc", "^abc", 1, 0, 0, nil, false, ""},
		{"anchored ^$ at end", "abc", "^$", 3, 3, 3, nil, true, ""},
		{"anchored ^ at end", "abc", "^", 3, 3, 3, nil, true, ""},
		{"frontier uses the real previous byte at init", "xa bc", "%f[%a]", 1, 3, 3, nil, true, ""},
		{"frontier at init 0 uses virtual NUL", "xa bc", "%f[%a]", 0, 0, 0, nil, true, ""},
		{"empty subject empty pattern", "", "", 0, 0, 0, nil, true, ""},
		{"empty subject a*", "", "a*", 0, 0, 0, nil, true, ""},
		{"empty subject a", "", "a", 0, 0, 0, nil, false, ""},
		{"empty subject $", "", "$", 0, 0, 0, nil, true, ""},
		{"empty subject %b()", "", "%b()", 0, 0, 0, nil, false, ""},
		{"empty subject capture", "", "(a*)", 0, 0, 0, [][2]int{{0, 0}}, true, ""},
		{"malformed pattern at init", "hello", "l[", 3, 0, 0, nil, false, "malformed pattern (missing ']')"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end, caps, ok, err := patMatch(tc.s, tc.pat, tc.init)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("patMatch(%q, %q, %d): error = %v, want one containing %q", tc.s, tc.pat, tc.init, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("patMatch(%q, %q, %d): unexpected error: %v", tc.s, tc.pat, tc.init, err)
			}
			if ok != tc.ok || start != tc.start || end != tc.end || !patCapsEqual(caps, tc.caps) {
				t.Fatalf("patMatch(%q, %q, %d) = (%d, %d, %v, %v), want (%d, %d, %v, %v)",
					tc.s, tc.pat, tc.init, start, end, caps, ok, tc.start, tc.end, tc.caps, tc.ok)
			}
		})
	}
}
