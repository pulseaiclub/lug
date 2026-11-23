// Command lug runs Lua scripts.
//
// Usage:
//
//	lug [options] script.lua [args...]
//	lug -e CODE [args...]
//	lug version
//
// Scripts are interpreted (no build step, no Go toolchain), and the argument
// list is passed to the chunk as `...` and through the global `arg` table,
// matching the reference interpreter.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pulseaiclub/lug"
)

const version = "0.2.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lug:", err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	if len(argv) == 0 {
		usage()
		return fmt.Errorf("no script given")
	}

	switch argv[0] {
	case "version", "-v", "--version":
		fmt.Println("lug", version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	}

	fs := flag.NewFlagSet("lug", flag.ExitOnError)
	code := fs.String("e", "", "execute `CODE` instead of a file")
	fs.Usage = usage
	if err := fs.Parse(argv); err != nil {
		return err
	}

	env := lug.New()
	var (
		src  string
		name string
		args []string
	)
	argZero, scriptName := "", "-e"
	if *code != "" {
		src, name, args = *code, "=(command line)", fs.Args()
	} else {
		script := fs.Arg(0)
		if script == "" {
			usage()
			return fmt.Errorf("no script given")
		}
		data, err := os.ReadFile(script)
		if err != nil {
			return err
		}
		src, name, args = string(data), "@"+script, fs.Args()[1:]
		argZero, scriptName = script, script
	}
	if argZero == "" {
		argZero = scriptName
	}

	// arg[0] is the script name, arg[1..] the arguments, like the reference
	// interpreter.
	argTable := lug.NewTable()
	argTable.Set(lug.Int(0), lug.String(argZero))
	for i, a := range args {
		argTable.Set(lug.Int(int64(i+1)), lug.String(a))
	}
	env.Set("arg", lug.TableVal(argTable))

	prog, err := lug.Compile(src, name)
	if err != nil {
		return err
	}
	vals := make([]lug.Value, len(args))
	for i, a := range args {
		vals[i] = lug.String(a)
	}
	if _, err := prog.Run(env, vals...); err != nil {
		return err
	}
	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `lug - Lua, embedded and interpreted

Usage:
  lug [options] script.lua [args...]
  lug -e `+"`CODE`"+` [args...]
  lug version

The chunk receives the arguments as ... and through the global arg table.
`)
}
