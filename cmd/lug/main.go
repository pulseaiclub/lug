// Command lug transpiles Lua source files to Go.
//
// Usage:
//
//	lug gen [-pkg NAME] [-o FILE] [-name VAR] file.lua...
//	    Transpile each file. Default package is "main"; the default output
//	    is <file>_lug.go next to the input; the default chunk var is
//	    Chunk_<basename>.
//
//	lug run [--] script.lua [args...]
//	    Transpile, compile and run a script in a temporary Go module
//	    (development loop). Requires the lug module to be locatable: run it
//	    inside a module that depends on lug, or set LUG_SRC to the lug
//	    source directory.
//
//	lug version
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pulseaiclub/lug/internal/codegen"
	"github.com/pulseaiclub/lug/internal/parser"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "gen":
		err = cmdGen(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("lug", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "lug: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lug:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `lug - Lua to Go transpiler

Usage:
  lug gen [-pkg NAME] [-o FILE] [-name VAR] file.lua...
  lug run [--] script.lua [args...]
  lug version
`)
}

// ---------------------------------------------------------------------------
// gen
// ---------------------------------------------------------------------------

func cmdGen(argv []string) error {
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	pkg := fs.String("pkg", "main", "Go package name of the generated file")
	out := fs.String("o", "", "output file (default: <input>_lug.go; single input only)")
	name := fs.String("name", "", "Go identifier for the chunk var (default: Chunk_<basename>)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	files := fs.Args()
	if len(files) == 0 {
		return fmt.Errorf("gen: no input files")
	}
	if *out != "" && len(files) > 1 {
		return fmt.Errorf("gen: -o accepts a single input file")
	}
	for _, file := range files {
		outFile := *out
		if outFile == "" {
			outFile = strings.TrimSuffix(file, filepath.Ext(file)) + "_lug.go"
		}
		chunkName := *name
		if chunkName == "" {
			base := filepath.Base(strings.TrimSuffix(file, filepath.Ext(file)))
			chunkName = "Chunk_" + sanitizeIdent(base)
		}
		if err := genFile(file, outFile, *pkg, chunkName); err != nil {
			return err
		}
		fmt.Printf("generated %s from %s\n", outFile, file)
	}
	return nil
}

func genFile(src, dst, pkg, chunkName string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	file, err := parser.Parse(data, src)
	if err != nil {
		return err
	}
	code, err := codegen.Generate(file, codegen.Options{
		Package:   pkg,
		ChunkName: chunkName,
		Source:    filepath.Base(src),
	})
	if err != nil {
		return err
	}
	return os.WriteFile(dst, code, 0o644)
}

func sanitizeIdent(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// run
// ---------------------------------------------------------------------------

func cmdRun(argv []string) error {
	var scriptArgs []string
	for i, a := range argv {
		if a == "--" {
			scriptArgs = argv[i+1:]
			argv = argv[:i]
			break
		}
	}
	if len(argv) == 0 {
		return fmt.Errorf("run: no script")
	}
	script := argv[0]
	scriptArgs = append(argv[1:], scriptArgs...)

	src, err := os.ReadFile(script)
	if err != nil {
		return err
	}
	file, err := parser.Parse(src, script)
	if err != nil {
		return err
	}
	code, err := codegen.Generate(file, codegen.Options{
		Package:   "main",
		ChunkName: "Chunk_main",
		Source:    filepath.Base(script),
	})
	if err != nil {
		return err
	}

	lugDir, err := findLugDir()
	if err != nil {
		return err
	}
	goVer, err := goDirective(filepath.Join(lugDir, "go.mod"))
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "lugrun")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	mainSrc := `package main

import (
	"fmt"
	"os"

	"github.com/pulseaiclub/lug"
	lg "github.com/pulseaiclub/lug/runtime"
)

func main() {
	env := lug.New()
	args := make([]lg.Value, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		args = append(args, lg.String(a))
	}
	if _, err := lug.Run(env, Chunk_main, args...); err != nil {
		fmt.Fprintln(os.Stderr, "lua error:", err)
		os.Exit(1)
	}
}
`
	files := map[string]string{
		"go.mod":       fmt.Sprintf("module lugrun\n\ngo %s\n\nrequire github.com/pulseaiclub/lug v0.0.0\n\nreplace github.com/pulseaiclub/lug => %s\n", goVer, filepath.ToSlash(lugDir)),
		"main.go":      mainSrc,
		"chunk_gen.go": string(code),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}

	cmd := exec.Command("go", append([]string{"run", "."}, scriptArgs...)...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// findLugDir locates the lug module source directory for `lug run`.
func findLugDir() (string, error) {
	if d := os.Getenv("LUG_SRC"); d != "" {
		return d, nil
	}
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/pulseaiclub/lug").Output()
	if err == nil {
		if d := strings.TrimSpace(string(out)); d != "" {
			return d, nil
		}
	}
	return "", fmt.Errorf("run: cannot locate the lug module source;\n" +
		"run inside a module that depends on github.com/pulseaiclub/lug, or set LUG_SRC=<lug source dir>")
}

// goDirective extracts the go version line from a go.mod file so the temp
// module satisfies the dependency's version requirement.
func goDirective(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "go ")), nil
		}
	}
	return "1.21", nil
}
