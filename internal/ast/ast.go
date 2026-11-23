// Package ast defines the Lua abstract syntax tree produced by the parser
// and consumed by the interpreter.
//
// The tree keeps Lua surface syntax (method calls, property access, multiple
// assignment); the code generator performs the desugaring, where it has
// full scope information.
package ast

// BinOp is a binary operator.
type BinOp int

// Binary operators, ordered by Lua precedence (low to high).
const (
	OpOr BinOp = iota
	OpAnd
	OpLT
	OpGT
	OpLE
	OpGE
	OpNE
	OpEQ
	OpBor
	OpBxor
	OpBand
	OpShl
	OpShr
	OpConcat
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpIDiv
	OpMod
	OpPow
)

// UnOp is a unary operator.
type UnOp int

// Unary operators.
const (
	UnNeg  UnOp = iota // -
	UnNot              // not
	UnLen              // #
	UnBnot             // ~
)

// Expr is a Lua expression node.
type Expr interface{ exprNode() }

// Stmt is a Lua statement node.
type Stmt interface{ stmtNode() }

// Block is a statement list with its own scope.
type Block struct {
	Stmts []Stmt
}

// File is a parsed chunk.
type File struct {
	Stmts []Stmt
}

// ---------------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------------

// NilExpr is `nil`.
type NilExpr struct{ Line int }

// BoolExpr is `true` or `false`.
type BoolExpr struct {
	Line int
	Val  bool
}

// NumberExpr is a numeric literal.
type NumberExpr struct {
	Line int
	Val  float64
}

// StringExpr is a string literal.
type StringExpr struct {
	Line int
	Val  string
}

// VarargExpr is `...`.
type VarargExpr struct{ Line int }

// NameExpr is an identifier (local, upvalue or global).
type NameExpr struct {
	Line int
	Name string
}

// IndexExpr is `obj[key]` (property access `a.b` is desugared to
// IndexExpr{obj, StringExpr{"b"}} by the parser).
type IndexExpr struct {
	Line int
	Obj  Expr
	Key  Expr
}

// CallExpr is `fn(args...)`.
type CallExpr struct {
	Line int
	Fn   Expr
	Args []Expr
}

// MethodExpr is `obj:name(args...)`.
type MethodExpr struct {
	Line int
	Obj  Expr
	Name string
	Args []Expr
}

// FuncExpr is an anonymous function.
type FuncExpr struct {
	Line   int
	Params []string
	Vararg bool
	Body   *Block
}

// Field is one entry of a table constructor; Key == nil means an array item.
type Field struct {
	Key Expr
	Val Expr
}

// TableExpr is a table constructor.
type TableExpr struct {
	Line   int
	Fields []Field
}

// BinExpr is a binary operation.
type BinExpr struct {
	Line int
	Op   BinOp
	L, R Expr
}

// UnExpr is a unary operation.
type UnExpr struct {
	Line int
	Op   UnOp
	X    Expr
}

// ParenExpr is a parenthesized expression. It matters semantically: it
// truncates multiple values to one ((f()) evaluates to f's first result).
type ParenExpr struct {
	Line int
	X    Expr
}

func (*NilExpr) exprNode()    {}
func (*BoolExpr) exprNode()   {}
func (*NumberExpr) exprNode() {}
func (*StringExpr) exprNode() {}
func (*VarargExpr) exprNode() {}
func (*NameExpr) exprNode()   {}
func (*IndexExpr) exprNode()  {}
func (*CallExpr) exprNode()   {}
func (*MethodExpr) exprNode() {}
func (*FuncExpr) exprNode()   {}
func (*TableExpr) exprNode()  {}
func (*BinExpr) exprNode()    {}
func (*UnExpr) exprNode()     {}
func (*ParenExpr) exprNode()  {}

// ---------------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------------

// LocalStmt is `local a, b = ...` (Vals may be empty).
type LocalStmt struct {
	Line  int
	Names []string
	Vals  []Expr
}

// AssignStmt is `a, t[k] = ...`.
type AssignStmt struct {
	Line    int
	Targets []Expr
	Vals    []Expr
}

// CallStmt is an expression statement (always a call).
type CallStmt struct {
	Line int
	Call Expr // *CallExpr or *MethodExpr
}

// DoStmt is `do ... end`.
type DoStmt struct {
	Line int
	Body *Block
}

// WhileStmt is `while cond do ... end`.
type WhileStmt struct {
	Line int
	Cond Expr
	Body *Block
}

// RepeatStmt is `repeat ... until cond` (cond sees body locals).
type RepeatStmt struct {
	Line int
	Body *Block
	Cond Expr
}

// IfStmt is `if c1 then ... elseif c2 then ... else ... end`.
type IfStmt struct {
	Line   int
	Conds  []Expr
	Blocks []*Block
	Else   *Block
}

// NumForStmt is `for i = init, limit [, step] do ... end`.
type NumForStmt struct {
	Line  int
	Var   string
	Init  Expr
	Limit Expr
	Step  Expr // nil means default step 1
	Body  *Block
}

// GenForStmt is `for a, b in f, s, c do ... end`.
type GenForStmt struct {
	Line  int
	Names []string
	Exprs []Expr
	Body  *Block
}

// FuncStmt is `function a.b.c(...)` or `function a.b:c(...)`.
type FuncStmt struct {
	Line   int
	Target Expr   // *NameExpr or *IndexExpr chain
	Method string // method name for `function a:b()` (self is Params[0]); "" otherwise
	Params []string
	Vararg bool
	Body   *Block
}

// LocalFuncStmt is `local function f(...)`.
type LocalFuncStmt struct {
	Line   int
	Name   string
	Params []string
	Vararg bool
	Body   *Block
}

// ReturnStmt is `return ...` (Vals may be empty).
type ReturnStmt struct {
	Line int
	Vals []Expr
}

// BreakStmt is `break`.
type BreakStmt struct{ Line int }

// GotoStmt is `goto label`.
type GotoStmt struct {
	Line  int
	Label string
}

// LabelStmt is `::label::`.
type LabelStmt struct {
	Line int
	Name string
}

func (*LocalStmt) stmtNode()     {}
func (*AssignStmt) stmtNode()    {}
func (*CallStmt) stmtNode()      {}
func (*DoStmt) stmtNode()        {}
func (*WhileStmt) stmtNode()     {}
func (*RepeatStmt) stmtNode()    {}
func (*IfStmt) stmtNode()        {}
func (*NumForStmt) stmtNode()    {}
func (*GenForStmt) stmtNode()    {}
func (*FuncStmt) stmtNode()      {}
func (*LocalFuncStmt) stmtNode() {}
func (*ReturnStmt) stmtNode()    {}
func (*BreakStmt) stmtNode()     {}
func (*GotoStmt) stmtNode()      {}
func (*LabelStmt) stmtNode()     {}

// IsCall reports whether e is a function call (usable as a statement).
func IsCall(e Expr) bool {
	switch e.(type) {
	case *CallExpr, *MethodExpr:
		return true
	}
	return false
}

// IsVar reports whether e is a valid assignment target.
func IsVar(e Expr) bool {
	switch e.(type) {
	case *NameExpr, *IndexExpr:
		return true
	}
	return false
}
