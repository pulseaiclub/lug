package lug

import (
	"fmt"
	"os"

	"github.com/pulseaiclub/lug/internal/interp"
	"github.com/pulseaiclub/lug/internal/parser"
	"github.com/pulseaiclub/lug/runtime"
)

// Run-time compilation.
//
// Lua source is parsed into an AST and interpreted; nothing is pre-compiled and
// no toolchain is needed at any point. A Script parses once and then runs as
// often as needed, including concurrently.
//
// Linking this package also enables the Lua-level load, loadfile and dofile
// functions, so a script can load further scripts by itself.

func init() {
	runtime.InstallLoader(func(source, chunkname string) (Fn, error) {
		file, err := parser.Parse([]byte(source), chunkname)
		if err != nil {
			return nil, err
		}
		return interp.Compile(file, chunkname).Chunk(), nil
	})
}

// Script is a Lua script compiled at run time. Parsing happens once, so a
// Script can be run repeatedly and concurrently.
type Script struct {
	prog *interp.Program
	name string
}

// Compile parses Lua source and prepares it for interpretation. name labels the
// chunk in error messages, like Lua's chunkname ("=(string)", "@file.lua").
func Compile(source, name string) (*Script, error) {
	prog, err := interp.Parse(source, name)
	if err != nil {
		return nil, err
	}
	return &Script{prog: prog, name: name}, nil
}

// CompileFile reads and compiles a Lua file.
func CompileFile(path string) (*Script, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Compile(string(data), "@"+path)
}

// Name returns the chunk name the script was compiled with.
func (s *Script) Name() string { return s.name }

// Fn returns the script as a callable value, so it can be stored in a table,
// bound as a callback, or wrapped in a coroutine.
func (s *Script) Fn() Fn { return s.prog.Chunk() }

// Run executes the script in env and returns its values. Lua errors come back
// as a Go error.
func (s *Script) Run(env *Env, args ...Value) ([]Value, error) {
	return s.prog.Run(env, args...)
}

// RunScript1 Run1 executes the script and converts its first result to T.
func RunScript1[T any](s *Script, env *Env, args ...Value) (T, error) {
	var zero T
	rets, err := s.Run(env, args...)
	if err != nil {
		return zero, err
	}
	if len(rets) == 0 {
		return zero, fmt.Errorf("lug: script %s returned no values, want %T", s.name, zero)
	}
	v, ok := As[T](rets[0])
	if !ok {
		return zero, fmt.Errorf("lug: script %s returned %s, want %T", s.name, rets[0].Type(), zero)
	}
	return v, nil
}

// DoString compiles and runs Lua source in env.
func DoString(env *Env, source string) ([]Value, error) {
	s, err := Compile(source, "=(string)")
	if err != nil {
		return nil, err
	}
	return s.Run(env)
}

// DoFile loads a Lua file and runs it in env.
func DoFile(env *Env, path string) ([]Value, error) {
	s, err := CompileFile(path)
	if err != nil {
		return nil, err
	}
	return s.Run(env)
}
