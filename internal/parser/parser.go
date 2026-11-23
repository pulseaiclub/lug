// Package parser turns Lua 5.4 source into an AST.
//
// It is a hand-written recursive-descent parser. Newlines are not tokens,
// so expression parsing is greedy across lines exactly like Lua's own
// parser: `a = b \n (c).d` parses as `a = b(c).d`.
package parser

import (
	"fmt"

	"github.com/pulseaiclub/lug/internal/ast"
	"github.com/pulseaiclub/lug/internal/lexer"
)

// Error is a syntax error with a position.
type Error struct {
	File string
	Line int
	Col  int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Msg) }

// unaryPri is the precedence of prefix operators (between `..` and `^`).
const unaryPri = 12

// binPri maps binary operator tokens to {left, right} precedence.
// Right-associative operators have right = left-1.
var binPri = map[lexer.Type][2]int{
	lexer.OR:  {1, 1},
	lexer.AND: {2, 2},
	lexer.LT:  {3, 3}, lexer.GT: {3, 3}, lexer.LE: {3, 3}, lexer.GE: {3, 3}, lexer.NE: {3, 3}, lexer.EQ: {3, 3},
	lexer.BOR:  {4, 4},
	lexer.BXOR: {5, 5},
	lexer.BAND: {6, 6},
	lexer.SHL:  {7, 7}, lexer.SHR: {7, 7},
	lexer.CONCAT: {9, 8},
	lexer.ADD:    {10, 10}, lexer.SUB: {10, 10},
	lexer.MUL: {11, 11}, lexer.DIV: {11, 11}, lexer.IDIV: {11, 11}, lexer.MOD: {11, 11},
	lexer.POW: {14, 13},
}

var binOpOf = map[lexer.Type]ast.BinOp{
	lexer.OR: ast.OpOr, lexer.AND: ast.OpAnd,
	lexer.LT: ast.OpLT, lexer.GT: ast.OpGT, lexer.LE: ast.OpLE, lexer.GE: ast.OpGE,
	lexer.NE: ast.OpNE, lexer.EQ: ast.OpEQ,
	lexer.BOR: ast.OpBor, lexer.BXOR: ast.OpBxor, lexer.BAND: ast.OpBand,
	lexer.SHL: ast.OpShl, lexer.SHR: ast.OpShr,
	lexer.CONCAT: ast.OpConcat,
	lexer.ADD:    ast.OpAdd, lexer.SUB: ast.OpSub,
	lexer.MUL: ast.OpMul, lexer.DIV: ast.OpDiv, lexer.IDIV: ast.OpIDiv, lexer.MOD: ast.OpMod,
	lexer.POW: ast.OpPow,
}

var unOpOf = map[lexer.Type]ast.UnOp{
	lexer.SUB:  ast.UnNeg,
	lexer.NOT:  ast.UnNot,
	lexer.LEN:  ast.UnLen,
	lexer.BXOR: ast.UnBnot,
}

type parser struct {
	lex    *lexer.Lexer
	tok    lexer.Token
	peeked *lexer.Token
	file   string

	// vararg tracks whether `...` is legal in the current function. The
	// main chunk is vararg; each function body pushes its own flag.
	vararg []bool
}

// Parse parses a Lua chunk. name is used in error messages.
func Parse(src []byte, name string) (f *ast.File, err error) {
	defer func() {
		if r := recover(); r != nil {
			if le, ok := r.(*lexer.Error); ok {
				err = &Error{File: name, Line: le.Line, Col: le.Col, Msg: le.Msg}
				return
			}
			if pe, ok := r.(*Error); ok {
				err = pe
				return
			}
			panic(r)
		}
	}()
	p := &parser{lex: lexer.New(src), file: name, vararg: []bool{true}}
	p.next()
	stmts, sawReturn := p.parseChunk()
	_ = sawReturn
	if p.tok.Type != lexer.EOF {
		p.errorf("'%s' expected", "<eof>")
	}
	p.checkLabels(&ast.Block{Stmts: stmts})
	return &ast.File{Stmts: stmts}, nil
}

func (p *parser) errorf(format string, args ...any) {
	panic(&Error{File: p.file, Line: p.tok.Line, Col: p.tok.Col, Msg: fmt.Sprintf(format, args...)})
}

func (p *parser) next() {
	if p.peeked != nil {
		p.tok = *p.peeked
		p.peeked = nil
		return
	}
	p.tok = p.lex.Next()
}

// peek returns the token after the current one without consuming it.
func (p *parser) peek() lexer.Token {
	if p.peeked == nil {
		t := p.lex.Next()
		p.peeked = &t
	}
	return *p.peeked
}

func (p *parser) check(t lexer.Type) bool { return p.tok.Type == t }

func (p *parser) accept(t lexer.Type) bool {
	if p.check(t) {
		p.next()
		return true
	}
	return false
}

func (p *parser) expect(t lexer.Type) {
	if !p.check(t) {
		p.errorf("'%s' expected near '%s'", t.Name(), p.tokText())
	}
	p.next()
}

// expectName consumes an identifier and returns it.
func (p *parser) expectName() string {
	if p.tok.Type != lexer.NAME {
		p.errorf("<name> expected near '%s'", p.tokText())
	}
	name := p.tok.Str
	p.next()
	return name
}

func (p *parser) tokText() string {
	switch p.tok.Type {
	case lexer.EOF:
		return "<eof>"
	case lexer.NAME, lexer.NUMBER, lexer.STRING:
		return p.tok.Str
	default:
		return p.tok.Type.Name()
	}
}

// ---------------------------------------------------------------------------
// Blocks and statements
// ---------------------------------------------------------------------------

func (p *parser) parseChunk() ([]ast.Stmt, bool) {
	return p.parseStmts(true)
}

// parseStmts parses statements until a block terminator. It reports whether
// the block ended with a return statement (which must be last).
func (p *parser) parseStmts(chunk bool) ([]ast.Stmt, bool) {
	var stmts []ast.Stmt
	for !p.atBlockEnd() {
		s := p.parseStatement()
		if s == nil {
			continue
		}
		stmts = append(stmts, s)
		if _, ok := s.(*ast.ReturnStmt); ok {
			// return must be the last statement of its block
			if p.check(lexer.SEMI) {
				p.next()
			}
			if !p.atBlockEnd() {
				p.errorf("'end' expected (return must be the last statement)")
			}
			return stmts, true
		}
	}
	return stmts, false
}

func (p *parser) atBlockEnd() bool {
	switch p.tok.Type {
	case lexer.EOF, lexer.END, lexer.ELSE, lexer.ELSEIF, lexer.UNTIL:
		return true
	}
	return false
}

func (p *parser) parseBlock() *ast.Block {
	stmts, _ := p.parseStmts(false)
	return &ast.Block{Stmts: stmts}
}

func (p *parser) parseStatement() ast.Stmt {
	line := p.tok.Line
	switch p.tok.Type {
	case lexer.SEMI:
		p.next()
		return nil
	case lexer.BREAK:
		p.next()
		return &ast.BreakStmt{Line: line}
	case lexer.GOTO:
		p.next()
		name := p.expectName()
		return &ast.GotoStmt{Line: line, Label: name}
	case lexer.DBCOLON:
		p.next()
		name := p.expectName()
		p.expect(lexer.DBCOLON)
		return &ast.LabelStmt{Line: line, Name: name}
	case lexer.DO:
		p.next()
		body := p.parseBlock()
		p.expect(lexer.END)
		return &ast.DoStmt{Line: line, Body: body}
	case lexer.WHILE:
		p.next()
		cond := p.parseExpr()
		p.expect(lexer.DO)
		body := p.parseBlock()
		p.expect(lexer.END)
		return &ast.WhileStmt{Line: line, Cond: cond, Body: body}
	case lexer.REPEAT:
		p.next()
		body := p.parseBlock()
		p.expect(lexer.UNTIL)
		cond := p.parseExpr()
		return &ast.RepeatStmt{Line: line, Body: body, Cond: cond}
	case lexer.IF:
		return p.parseIf()
	case lexer.FOR:
		return p.parseFor()
	case lexer.FUNCTION:
		p.next()
		return p.parseFuncStat(line)
	case lexer.LOCAL:
		return p.parseLocal(line)
	case lexer.RETURN:
		return p.parseReturn(line)
	}
	return p.parseExprStat(line)
}

func (p *parser) parseIf() ast.Stmt {
	line := p.tok.Line
	p.next() // if
	v := &ast.IfStmt{Line: line}
	for {
		cond := p.parseExpr()
		p.expect(lexer.THEN)
		v.Conds = append(v.Conds, cond)
		v.Blocks = append(v.Blocks, p.parseBlock())
		if p.check(lexer.ELSEIF) {
			p.next()
			continue
		}
		break
	}
	if p.accept(lexer.ELSE) {
		v.Else = p.parseBlock()
	}
	p.expect(lexer.END)
	return v
}

func (p *parser) parseFor() ast.Stmt {
	line := p.tok.Line
	p.next() // for
	name1 := p.expectName()
	if p.accept(lexer.ASSIGN) {
		// numeric for
		init := p.parseExpr()
		p.expect(lexer.COMMA)
		limit := p.parseExpr()
		var step ast.Expr
		if p.accept(lexer.COMMA) {
			step = p.parseExpr()
		}
		p.expect(lexer.DO)
		body := p.parseBlock()
		p.expect(lexer.END)
		return &ast.NumForStmt{Line: line, Var: name1, Init: init, Limit: limit, Step: step, Body: body}
	}
	// generic for
	names := []string{name1}
	for p.accept(lexer.COMMA) {
		names = append(names, p.expectName())
	}
	p.expect(lexer.IN)
	exprs := p.parseExprList()
	p.expect(lexer.DO)
	body := p.parseBlock()
	p.expect(lexer.END)
	return &ast.GenForStmt{Line: line, Names: names, Exprs: exprs, Body: body}
}

// parseFuncStat parses `function Name {'.' Name} [':' Name] funcbody`.
// The `function` keyword is already consumed.
func (p *parser) parseFuncStat(line int) ast.Stmt {
	var target ast.Expr = &ast.NameExpr{Line: line, Name: p.expectName()}
	for p.accept(lexer.DOT) {
		name := p.expectName()
		target = &ast.IndexExpr{Line: line, Obj: target, Key: &ast.StringExpr{Line: line, Val: name}}
	}
	method := ""
	if p.accept(lexer.COLON) {
		method = p.expectName()
	}
	params, vararg, body := p.parseFuncBody(line)
	if method != "" {
		// implicit self
		params = append([]string{"self"}, params...)
	}
	return &ast.FuncStmt{Line: line, Target: target, Method: method, Params: params, Vararg: vararg, Body: body}
}

func (p *parser) parseLocal(line int) ast.Stmt {
	p.next() // local
	if p.accept(lexer.FUNCTION) {
		name := p.expectName()
		params, vararg, body := p.parseFuncBody(line)
		return &ast.LocalFuncStmt{Line: line, Name: name, Params: params, Vararg: vararg, Body: body}
	}
	var names []string
	for {
		names = append(names, p.expectName())
		p.parseLocalAttrib()
		if !p.accept(lexer.COMMA) {
			break
		}
	}
	var vals []ast.Expr
	if p.accept(lexer.ASSIGN) {
		vals = p.parseExprList()
	}
	return &ast.LocalStmt{Line: line, Names: names, Vals: vals}
}

// parseLocalAttrib consumes `<const>` / `<close>` after a local name.
// const is accepted (and treated like a plain local); close is rejected
// with a clear message until to-be-closed variables are supported.
func (p *parser) parseLocalAttrib() {
	if !p.accept(lexer.LT) {
		return
	}
	attr := p.expectName()
	p.expect(lexer.GT)
	switch attr {
	case "const":
		// treated like a plain local
	case "close":
		p.errorf("to-be-closed variables (<close>) are not supported yet")
	default:
		p.errorf("unknown attribute '%s'", attr)
	}
}

func (p *parser) parseReturn(line int) ast.Stmt {
	p.next() // return
	var vals []ast.Expr
	if !p.atBlockEnd() && !p.check(lexer.SEMI) {
		vals = p.parseExprList()
	}
	return &ast.ReturnStmt{Line: line, Vals: vals}
}

// parseFuncBody parses `(parlist) block end`.
func (p *parser) parseFuncBody(line int) (params []string, vararg bool, body *ast.Block) {
	p.expect(lexer.LPAREN)
	if !p.check(lexer.RPAREN) {
		for {
			if p.check(lexer.VARARG) {
				p.next()
				vararg = true
				break
			}
			params = append(params, p.expectName())
			if !p.accept(lexer.COMMA) {
				break
			}
		}
	}
	p.expect(lexer.RPAREN)
	p.vararg = append(p.vararg, vararg)
	body = p.parseBlock()
	p.vararg = p.vararg[:len(p.vararg)-1]
	p.expect(lexer.END)
	p.checkLabels(body)
	return params, vararg, body
}

// checkLabels verifies that every goto in a function body has a matching
// label in the same function (Lua's label visibility, approximated).
func (p *parser) checkLabels(b *ast.Block) {
	labels := map[string]bool{}
	var gotos []string
	var walk func(stmts []ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, s := range stmts {
			switch v := s.(type) {
			case *ast.LabelStmt:
				labels[v.Name] = true
			case *ast.GotoStmt:
				gotos = append(gotos, v.Label)
			case *ast.DoStmt:
				walk(v.Body.Stmts)
			case *ast.WhileStmt:
				walk(v.Body.Stmts)
			case *ast.RepeatStmt:
				walk(v.Body.Stmts)
			case *ast.IfStmt:
				for _, blk := range v.Blocks {
					walk(blk.Stmts)
				}
				if v.Else != nil {
					walk(v.Else.Stmts)
				}
			case *ast.NumForStmt:
				walk(v.Body.Stmts)
			case *ast.GenForStmt:
				walk(v.Body.Stmts)
			}
		}
	}
	walk(b.Stmts)
	for _, g := range gotos {
		if !labels[g] {
			p.errorf("no visible label '%s' for goto", g)
		}
	}
}

func (p *parser) parseExprStat(line int) ast.Stmt {
	e := p.parseSuffixedExpr()
	switch {
	case p.check(lexer.ASSIGN) || p.check(lexer.COMMA):
		targets := []ast.Expr{e}
		for p.accept(lexer.COMMA) {
			targets = append(targets, p.parseSuffixedExpr())
		}
		p.expect(lexer.ASSIGN)
		for _, t := range targets {
			if !ast.IsVar(t) {
				p.errorf("syntax error near '%s'", exprName(t))
			}
		}
		vals := p.parseExprList()
		return &ast.AssignStmt{Line: line, Targets: targets, Vals: vals}
	case ast.IsCall(e):
		return &ast.CallStmt{Line: line, Call: e}
	}
	p.errorf("syntax error unexpected symbol near '%s'", exprName(e))
	return nil
}

func exprName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.NameExpr:
		return v.Name
	case *ast.StringExpr:
		return v.Val
	case *ast.NumberExpr:
		return "number"
	}
	return "expression"
}

// ---------------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------------

func (p *parser) parseExprList() []ast.Expr {
	list := []ast.Expr{p.parseExpr()}
	for p.accept(lexer.COMMA) {
		list = append(list, p.parseExpr())
	}
	return list
}

// parseSubExpr implements precedence climbing:
//
//	subexpr(limit) = (unop subexpr(unaryPri) | simpleexp) { binop subexpr(rightpri) }
func (p *parser) parseSubExpr(limit int) ast.Expr {
	var left ast.Expr
	if op, ok := unOpOf[p.tok.Type]; ok {
		line := p.tok.Line
		p.next()
		operand := p.parseSubExpr(unaryPri)
		left = &ast.UnExpr{Line: line, Op: op, X: operand}
	} else {
		left = p.parseSimpleExpr()
	}
	for {
		pri, ok := binPri[p.tok.Type]
		if !ok || pri[0] <= limit {
			return left
		}
		op := binOpOf[p.tok.Type]
		line := p.tok.Line
		p.next()
		right := p.parseSubExpr(pri[1])
		left = &ast.BinExpr{Line: line, Op: op, L: left, R: right}
	}
}

func (p *parser) parseExpr() ast.Expr { return p.parseSubExpr(0) }

func (p *parser) parseSimpleExpr() ast.Expr {
	line := p.tok.Line
	switch p.tok.Type {
	case lexer.NUMBER:
		v := &ast.NumberExpr{Line: line, Val: p.tok.Num}
		p.next()
		return v
	case lexer.STRING:
		v := &ast.StringExpr{Line: line, Val: p.tok.Str}
		p.next()
		// Lua concatenates adjacent string literals ("a" "b" == "ab").
		for p.check(lexer.STRING) {
			v.Val += p.tok.Str
			p.next()
		}
		return v
	case lexer.NIL:
		p.next()
		return &ast.NilExpr{Line: line}
	case lexer.TRUE:
		p.next()
		return &ast.BoolExpr{Line: line, Val: true}
	case lexer.FALSE:
		p.next()
		return &ast.BoolExpr{Line: line, Val: false}
	case lexer.VARARG:
		p.next()
		if !p.vararg[len(p.vararg)-1] {
			p.errorf("cannot use '...' outside a vararg function")
		}
		return &ast.VarargExpr{Line: line}
	case lexer.LBRACE:
		return p.parseTableConstructor()
	case lexer.FUNCTION:
		p.next()
		params, vararg, body := p.parseFuncBody(line)
		return &ast.FuncExpr{Line: line, Params: params, Vararg: vararg, Body: body}
	}
	return p.parseSuffixedExpr()
}

// parsePrimaryExpr parses a Name or a parenthesized expression.
func (p *parser) parsePrimaryExpr() ast.Expr {
	line := p.tok.Line
	switch p.tok.Type {
	case lexer.NAME:
		name := p.tok.Str
		p.next()
		return &ast.NameExpr{Line: line, Name: name}
	case lexer.LPAREN:
		p.next()
		x := p.parseExpr()
		p.expect(lexer.RPAREN)
		return &ast.ParenExpr{Line: line, X: x}
	}
	p.errorf("unexpected symbol near '%s'", p.tokText())
	return nil
}

// parseSuffixedExpr parses a primary expression followed by `.name`,
// `[exp]`, `:name args` or `args` suffixes.
func (p *parser) parseSuffixedExpr() ast.Expr {
	e := p.parsePrimaryExpr()
	for {
		line := p.tok.Line
		switch p.tok.Type {
		case lexer.DOT:
			p.next()
			name := p.expectName()
			e = &ast.IndexExpr{Line: line, Obj: e, Key: &ast.StringExpr{Line: line, Val: name}}
		case lexer.LBRACK:
			p.next()
			key := p.parseExpr()
			p.expect(lexer.RBRACK)
			e = &ast.IndexExpr{Line: line, Obj: e, Key: key}
		case lexer.COLON:
			p.next()
			name := p.expectName()
			args := p.parseCallArgs()
			e = &ast.MethodExpr{Line: line, Obj: e, Name: name, Args: args}
		case lexer.LPAREN, lexer.STRING, lexer.LBRACE:
			args := p.parseCallArgs()
			e = &ast.CallExpr{Line: line, Fn: e, Args: args}
		default:
			return e
		}
	}
}

// parseCallArgs parses `(explist)`, a table constructor or a string literal.
func (p *parser) parseCallArgs() []ast.Expr {
	line := p.tok.Line
	switch p.tok.Type {
	case lexer.LPAREN:
		p.next()
		if p.accept(lexer.RPAREN) {
			return nil
		}
		args := p.parseExprList()
		p.expect(lexer.RPAREN)
		return args
	case lexer.STRING:
		v := &ast.StringExpr{Line: line, Val: p.tok.Str}
		p.next()
		return []ast.Expr{v}
	case lexer.LBRACE:
		return []ast.Expr{p.parseTableConstructor()}
	}
	p.errorf("function arguments expected near '%s'", p.tokText())
	return nil
}

func (p *parser) parseTableConstructor() ast.Expr {
	line := p.tok.Line
	p.expect(lexer.LBRACE)
	t := &ast.TableExpr{Line: line}
	for !p.check(lexer.RBRACE) {
		switch {
		case p.check(lexer.LBRACK):
			p.next()
			key := p.parseExpr()
			p.expect(lexer.RBRACK)
			p.expect(lexer.ASSIGN)
			val := p.parseExpr()
			t.Fields = append(t.Fields, ast.Field{Key: key, Val: val})
		case p.check(lexer.NAME) && p.peek().Type == lexer.ASSIGN:
			key := &ast.StringExpr{Line: p.tok.Line, Val: p.tok.Str}
			p.next()
			p.next()
			val := p.parseExpr()
			t.Fields = append(t.Fields, ast.Field{Key: key, Val: val})
		default:
			val := p.parseExpr()
			t.Fields = append(t.Fields, ast.Field{Val: val})
		}
		if !p.accept(lexer.COMMA) && !p.accept(lexer.SEMI) {
			break
		}
	}
	p.expect(lexer.RBRACE)
	return t
}
