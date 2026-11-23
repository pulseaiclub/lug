package runtime

// Env is a Lua environment: a globals table plus the string library used for
// string indexing ("abc":upper()). Environments are independent; create one
// per Lua state. A single Env must be used from one goroutine at a time
// (coroutines created by lib_coroutine are serialized by design, so Lua code
// never runs concurrently inside one Env).
type Env struct {
	globals *Table
	strlib  *Table
}

// installers register standard library pieces into new Envs. Each lib file
// appends to it from its own init function.
var installers []func(*Env)

// New creates an Env with the full standard library installed.
func New() *Env {
	e := &Env{globals: NewTable()}
	e.strlib = NewTable()
	e.Set("string", TableVal(e.strlib))
	for _, f := range installers {
		f(e)
	}
	return e
}

// Get returns a global variable (nil if absent).
func (e *Env) Get(name string) Value { return e.globals.Get(String(name)) }

// Set assigns a global variable.
func (e *Env) Set(name string, v Value) { e.globals.Set(String(name), v) }

// Globals returns the globals table as a Value (_G).
func (e *Env) Globals() Value { return TableVal(e.globals) }

// StringLib returns the string library table (used for string indexing).
func (e *Env) StringLib() *Table { return e.strlib }
