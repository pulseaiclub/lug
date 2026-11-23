package runtime

import (
	"fmt"
	"os"
)

// LoaderFunc compiles Lua source into a callable at run time. The lug package
// installs one backed by the AST interpreter, so importing lug is enough to
// make load, loadfile and dofile available; a program that uses only the
// runtime package does not pay for the interpreter and does not get them.
type LoaderFunc func(source, chunkname string) (Fn, error)

var loader LoaderFunc

// InstallLoader registers the run-time compiler used by load, loadfile and
// dofile. Environments created afterwards get those globals; existing ones are
// left alone. It must be called before New, which the lug package does from
// init.
func InstallLoader(f LoaderFunc) { loader = f }

// HasLoader reports whether a run-time compiler is installed.
func HasLoader() bool { return loader != nil }

func init() { installers = append(installers, installLoad) }

// installLoad registers the run-time loaders when a compiler is available.
func installLoad(e *Env) {
	if loader == nil {
		return
	}
	reg(e, "load", baseLoad)
	reg(e, "loadfile", baseLoadFile)
	reg(e, "dofile", baseDoFile)
}

// baseLoad implements load(chunk [, chunkname]) for string chunks.
func baseLoad(env *Env, args []Value) []Value {
	src, ok := strOf(Arg(args, 0))
	if !ok {
		Errorf("bad argument #1 to 'load' (string expected, got %s)", Arg(args, 0).Type())
	}
	name := "=(load)"
	if n, ok := strOf(Arg(args, 1)); ok {
		name = n
	}
	fn, err := loader(src, name)
	if err != nil {
		return []Value{Nil, String(err.Error())}
	}
	return []Value{NewClosure(fn, name)}
}

// baseLoadFile implements loadfile([filename]).
func baseLoadFile(env *Env, args []Value) []Value {
	fn, err := loadFile(env, args)
	if err != nil {
		return []Value{Nil, String(err.Error())}
	}
	return []Value{fn}
}

// baseDoFile implements dofile([filename]): it runs the chunk and returns its
// results, raising on errors like a normal call.
func baseDoFile(env *Env, args []Value) []Value {
	fn, err := loadFile(env, args)
	if err != nil {
		Error(String(err.Error()))
	}
	return Call(env, fn, nil)
}

func loadFile(env *Env, args []Value) (Value, error) {
	path, ok := strOf(Arg(args, 0))
	if !ok {
		return Nil, fmt.Errorf("bad argument #1 to 'loadfile' (string expected, got %s)", Arg(args, 0).Type())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Nil, err
	}
	fn, err := loader(string(data), "@"+path)
	if err != nil {
		return Nil, err
	}
	return NewClosure(fn, "@"+path), nil
}
