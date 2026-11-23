package runtime

import "os"

func init() { installers = append(installers, installIO) }

// installIO installs a minimal io library (stdout writing). Reading and
// file operations are intentionally omitted; scripts use print/io.write.
func installIO(e *Env) {
	io := NewTable()
	io.Set(String("write"), NewClosure(ioWrite, "io.write"))
	e.Set("io", TableVal(io))
}

func ioWrite(env *Env, args []Value) []Value {
	for _, a := range args {
		os.Stdout.WriteString(ToStringEnv(env, a))
	}
	return nil
}
