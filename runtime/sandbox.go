package runtime

// SandboxOptions selects the host capabilities a sandboxed environment gets.
// Nothing is granted by default: the pure parts of the standard library
// (string, table, math, coroutine and the base functions) are always there,
// while os, io and print must be opted into, and even then only a documented
// subset is exposed — no os.execute, os.remove, os.getenv or os.exit, whatever
// the options say.
type SandboxOptions struct {
	// OS exposes os.time, os.date, os.clock and os.difftime.
	OS bool
	// IO exposes io.write.
	IO bool
	// AllowPrint keeps the print global. Without OnPrint the output is dropped.
	AllowPrint bool
	// OnPrint receives one call per print, already formatted like Lua's print
	// (tab separated, no trailing newline). tag identifies the script.
	OnPrint func(tag, line string)
}

// NewSandboxed returns a fresh environment with only the capabilities named in
// opts. Each call allocates its own globals table, so two sandboxes never see
// each other's variables, which is what makes it safe to run scripts from
// different authors in one process.
//
// The sandbox restricts capabilities, not resources: a script that loops
// forever keeps its goroutine busy, because transpiled loops are ordinary Go
// loops with nothing to check between iterations, and interpreted loops are Go
// recursion over the AST. Run untrusted code in a separate process when it must
// be interruptible.
func NewSandboxed(tag string, opts SandboxOptions) *Env {
	e := New()

	// Denied explicitly even when the library does not define them today, so a
	// later addition cannot silently widen the sandbox. load/loadfile/dofile
	// are installed by the lug package, so they must be removed here.
	for _, name := range []string{"dofile", "load", "loadfile", "require", "debug", "package"} {
		e.Set(name, Nil)
	}

	if opts.OS {
		e.Set("os", subset(e.Get("os"), "time", "date", "clock", "difftime"))
	} else {
		e.Set("os", Nil)
	}
	if opts.IO {
		e.Set("io", subset(e.Get("io"), "write"))
	} else {
		e.Set("io", Nil)
	}

	switch {
	case !opts.AllowPrint:
		e.Set("print", Nil)
	default:
		onPrint, tag := opts.OnPrint, tag
		e.Set("print", NewClosure(func(env *Env, args []Value) []Value {
			if onPrint != nil {
				onPrint(tag, PrintLine(env, args))
			}
			return nil
		}, "print"))
	}
	return e
}

// subset copies the named fields of a library table into a new table. A
// missing source table yields an empty table.
func subset(lib Value, fields ...string) Value {
	t := lib.Table()
	out := NewTable()
	if t == nil {
		return TableVal(out)
	}
	for _, f := range fields {
		if v := t.Get(String(f)); !v.IsNil() {
			out.Set(String(f), v)
		}
	}
	return TableVal(out)
}
