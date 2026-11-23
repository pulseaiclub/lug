// Command embed shows how to use Lua as a scripting language at run time:
// scripts are read from a directory while the program runs, each one gets its
// own sandboxed environment, and a failing script cannot take the host down.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pulseaiclub/lug"
)

// script is one loaded plugin: its name, its environment and the chunk's
// return values (unused here, but a script may return a table of callbacks).
type script struct {
	name string
	env  *lug.Env
}

func main() {
	dir := "scripts"
	files, err := scriptFiles(dir)
	if err != nil {
		log.Fatal(err)
	}

	var loaded []script
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".lua")

		// One sandbox per script: separate globals, no os.execute, no io, no
		// load. print is routed to the host so the output is tagged.
		env := lug.NewSandboxed(name, lug.SandboxOptions{
			AllowPrint: true,
			OnPrint:    func(tag, line string) { fmt.Printf("[%s] %s\n", tag, line) },
		})
		env.Set("name", lug.ValueOf(name))

		// Compiling and running are two steps: parse once, run as often as
		// you like. A syntax error here is reported, not fatal.
		prog, err := lug.CompileFile(path)
		if err != nil {
			fmt.Printf("skip %s: %v\n", name, err)
			continue
		}
		if _, err := prog.Run(env); err != nil {
			fmt.Printf("skip %s: %v\n", name, err)
			continue
		}
		sc := script{name: name, env: env}
		if err := emit(sc, "on_load"); err != nil {
			fmt.Printf("hook error in %s: %v\n", name, err)
		}
		loaded = append(loaded, sc)
	}

	// Hooks are ordinary globals; the host decides when to call them and
	// treats an error as a log line, not as a crash.
	for frame := 1; frame <= 3; frame++ {
		for _, s := range loaded {
			if err := emit(s, "on_tick", lug.Number(0.5)); err != nil {
				fmt.Printf("hook error in %s: %v\n", s.name, err)
			}
		}
	}

	for _, s := range loaded {
		if err := emit(s, "on_unload"); err != nil {
			fmt.Printf("hook error in %s: %v\n", s.name, err)
		}
	}
}

// emit calls a global hook if the script defined one.
func emit(s script, hook string, args ...lug.Value) error {
	fn := s.env.Get(hook)
	if fn.IsNil() {
		return nil
	}
	_, err := lug.CallErr(s.env, fn, args...)
	return err
}

func scriptFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".lua") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}
