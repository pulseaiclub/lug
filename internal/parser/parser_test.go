package parser

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/lug/internal/ast"
)

func mustParse(t *testing.T, src string) *ast.File {
	t.Helper()
	f, err := Parse([]byte(src), "test.lua")
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return f
}

// TestNewlineAmbiguity locks the Lua statement-boundary rule the user
// explicitly called out: a newline is not a terminator, expression parsing
// is greedy, so `a = b \n (c).d` is `a = b(c).d`.
func TestNewlineAmbiguity(t *testing.T) {
	f := mustParse(t, "a = b\n(c).d\n")
	if len(f.Stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(f.Stmts))
	}
	as, ok := f.Stmts[0].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("expected assignment, got %T", f.Stmts[0])
	}
	if len(as.Targets) != 1 || as.Targets[0].(*ast.NameExpr).Name != "a" {
		t.Fatalf("bad target: %#v", as.Targets)
	}
	// value must be IndexExpr(CallExpr(b, c), "d")
	idx, ok := as.Vals[0].(*ast.IndexExpr)
	if !ok {
		t.Fatalf("expected index expr, got %T", as.Vals[0])
	}
	call, ok := idx.Obj.(*ast.CallExpr)
	if !ok {
		t.Fatalf("expected call, got %T", idx.Obj)
	}
	if call.Fn.(*ast.NameExpr).Name != "b" || len(call.Args) != 1 {
		t.Fatalf("bad call: %#v", call)
	}
	if idx.Key.(*ast.StringExpr).Val != "d" {
		t.Fatalf("bad key: %#v", idx.Key)
	}
}

func TestNewlineNotTerminatorBinary(t *testing.T) {
	// `a = b \n - c` is `a = b - c`
	f := mustParse(t, "a = b\n-c\n")
	as := f.Stmts[0].(*ast.AssignStmt)
	bin, ok := as.Vals[0].(*ast.BinExpr)
	if !ok || bin.Op != ast.OpSub {
		t.Fatalf("expected b - c, got %#v", as.Vals[0])
	}
}

func TestSemicolonSplits(t *testing.T) {
	// `a = b; (c).d()` is two statements
	f := mustParse(t, "a = b;\n(c).d()\n")
	if len(f.Stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(f.Stmts))
	}
	if _, ok := f.Stmts[1].(*ast.CallStmt); !ok {
		t.Fatalf("expected call statement, got %T", f.Stmts[1])
	}
}

func TestPrecedence(t *testing.T) {
	cases := []struct {
		src   string
		check func(t *testing.T, e ast.Expr)
	}{
		{"x = -a ^ 2", func(t *testing.T, e ast.Expr) {
			// -(a^2), not (-a)^2
			u, ok := e.(*ast.UnExpr)
			if !ok || u.Op != ast.UnNeg {
				t.Fatalf("expected unary neg, got %#v", e)
			}
			b, ok := u.X.(*ast.BinExpr)
			if !ok || b.Op != ast.OpPow {
				t.Fatalf("expected ^ under unary, got %#v", u.X)
			}
		}},
		{"x = not a == b", func(t *testing.T, e ast.Expr) {
			// (not a) == b
			b, ok := e.(*ast.BinExpr)
			if !ok || b.Op != ast.OpEQ {
				t.Fatalf("expected ==, got %#v", e)
			}
			if u, ok := b.L.(*ast.UnExpr); !ok || u.Op != ast.UnNot {
				t.Fatalf("expected (not a) == b, got %#v", b.L)
			}
		}},
		{"x = 1 .. 2 + 3", func(t *testing.T, e ast.Expr) {
			// 1 .. (2+3)
			b, ok := e.(*ast.BinExpr)
			if !ok || b.Op != ast.OpConcat {
				t.Fatalf("expected concat, got %#v", e)
			}
			if r, ok := b.R.(*ast.BinExpr); !ok || r.Op != ast.OpAdd {
				t.Fatalf("expected 2+3 on the right, got %#v", b.R)
			}
		}},
		{"x = a or b and c", func(t *testing.T, e ast.Expr) {
			// a or (b and c)
			b, ok := e.(*ast.BinExpr)
			if !ok || b.Op != ast.OpOr {
				t.Fatalf("expected or, got %#v", e)
			}
			if r, ok := b.R.(*ast.BinExpr); !ok || r.Op != ast.OpAnd {
				t.Fatalf("expected b and c on the right, got %#v", b.R)
			}
		}},
		{"x = a < b and b < c", func(t *testing.T, e ast.Expr) {
			b, ok := e.(*ast.BinExpr)
			if !ok || b.Op != ast.OpAnd {
				t.Fatalf("expected and, got %#v", e)
			}
			if l, ok := b.L.(*ast.BinExpr); !ok || l.Op != ast.OpLT {
				t.Fatalf("expected a < b, got %#v", b.L)
			}
		}},
		{"x = 2 ^ -3", func(t *testing.T, e ast.Expr) {
			b, ok := e.(*ast.BinExpr)
			if !ok || b.Op != ast.OpPow {
				t.Fatalf("expected pow, got %#v", e)
			}
			if _, ok := b.R.(*ast.UnExpr); !ok {
				t.Fatalf("expected unary on rhs of ^, got %#v", b.R)
			}
		}},
		{"x = a .. b .. c", func(t *testing.T, e ast.Expr) {
			// right associative: a .. (b .. c)
			b, ok := e.(*ast.BinExpr)
			if !ok || b.Op != ast.OpConcat {
				t.Fatalf("expected concat, got %#v", e)
			}
			if r, ok := b.R.(*ast.BinExpr); !ok || r.Op != ast.OpConcat {
				t.Fatalf("expected right-assoc concat, got %#v", b.R)
			}
		}},
	}
	for _, c := range cases {
		f := mustParse(t, c.src)
		as := f.Stmts[0].(*ast.AssignStmt)
		c.check(t, as.Vals[0])
	}
}

func TestSyntaxShapes(t *testing.T) {
	f := mustParse(t, `
		local function fact(n) if n <= 1 then return 1 end return n * fact(n-1) end
		local t = { 1, 2; x = 3, ["y"] = 4, [5+1] = 6, fact }
		function t.m(a, b) return a + b end
		function t:method() return self.x end
		local ok, err = pcall(function() error("x") end)
		for i = 1, 10, 2 do print(i) end
		for k, v in pairs(t) do print(k, v) end
		repeat local z = 1 until z == 1
		while true do break end
		goto skip
		::skip::
		do end
		local a <const> = 1
		return a, t[1], select("#", ...)
	`)
	if len(f.Stmts) != 14 {
		t.Fatalf("expected 14 statements, got %d: %#v", len(f.Stmts), f.Stmts)
	}
	last := f.Stmts[len(f.Stmts)-1].(*ast.ReturnStmt)
	if len(last.Vals) != 3 {
		t.Fatalf("expected 3 return values, got %d", len(last.Vals))
	}
}

func TestMethodSugarAndCallSugar(t *testing.T) {
	f := mustParse(t, `obj:m "lit" ; f{1}; ("x"):upper()`)
	cs := f.Stmts[0].(*ast.CallStmt).Call.(*ast.MethodExpr)
	if cs.Name != "m" || len(cs.Args) != 1 {
		t.Fatalf("bad method call: %#v", cs)
	}
	if _, ok := cs.Args[0].(*ast.StringExpr); !ok {
		t.Fatalf("string call sugar arg expected, got %#v", cs.Args[0])
	}
	call2 := f.Stmts[1].(*ast.CallStmt).Call.(*ast.CallExpr)
	if _, ok := call2.Args[0].(*ast.TableExpr); !ok {
		t.Fatalf("table call sugar arg expected, got %#v", call2.Args[0])
	}
	m3 := f.Stmts[2].(*ast.CallStmt).Call.(*ast.MethodExpr)
	if _, ok := m3.Obj.(*ast.ParenExpr); !ok {
		t.Fatalf("expected paren receiver, got %#v", m3.Obj)
	}
}

func TestAdjacentStringLiterals(t *testing.T) {
	f := mustParse(t, `s = "a" "b" 'c'`)
	v := f.Stmts[0].(*ast.AssignStmt).Vals[0].(*ast.StringExpr)
	if v.Val != "abc" {
		t.Fatalf("expected abc, got %q", v.Val)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		`x = `,
		`if true then `,
		`return 1 return 2`,
		`x = 1 +`,
		`local x <close> = 1`,
		`f = function(...) return ... end; g = function() return ... end`,
		`x = {1, 2`,
		`a.b:c(`,
		`::x:: goto y`,
	}
	for _, src := range cases {
		if _, err := Parse([]byte(src), "err.lua"); err == nil {
			t.Errorf("expected error parsing %q", src)
		} else if !strings.Contains(err.Error(), "err.lua:") {
			t.Errorf("error without file prefix: %v", err)
		}
	}
}

func TestGotoLabelValid(t *testing.T) {
	f := mustParse(t, `
		for i = 1, 10 do
			if i % 2 == 0 then goto continue end
			print(i)
			::continue::
		end
	`)
	if len(f.Stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(f.Stmts))
	}
}

func TestLongStringsAndNumbers(t *testing.T) {
	f := mustParse(t, "a = [[x\ny]] b = [==[z]==] c = 0xFF d = 0x1p4 e = 1e-3 f = .5")
	vals := make([]ast.Expr, 6)
	for i := range vals {
		vals[i] = f.Stmts[i].(*ast.AssignStmt).Vals[0]
	}
	if s := vals[0].(*ast.StringExpr).Val; s != "x\ny" {
		t.Errorf("long string: %q", s)
	}
	if s := vals[1].(*ast.StringExpr).Val; s != "z" {
		t.Errorf("leveled long string: %q", s)
	}
	if n := vals[2].(*ast.NumberExpr).Val; n != 255 {
		t.Errorf("hex: %v", n)
	}
	if n := vals[3].(*ast.NumberExpr).Val; n != 16 {
		t.Errorf("hex float: %v", n)
	}
	if n := vals[4].(*ast.NumberExpr).Val; n != 0.001 {
		t.Errorf("exp: %v", n)
	}
	if n := vals[5].(*ast.NumberExpr).Val; n != 0.5 {
		t.Errorf("dot float: %v", n)
	}
}

func TestStringEscapes(t *testing.T) {
	f := mustParse(t, `a = "\65\066\x43\u{44}\n\t\\\"z" b = "a\z
    b"`)
	v1 := f.Stmts[0].(*ast.AssignStmt).Vals[0].(*ast.StringExpr).Val
	if v1 != "ABCD\n\t\\\"z" {
		t.Errorf("escapes: %q", v1)
	}
	v2 := f.Stmts[1].(*ast.AssignStmt).Vals[0].(*ast.StringExpr).Val
	if v2 != "ab" {
		t.Errorf("\\z escape: %q", v2)
	}
}
