package runtime

import "fmt"

// LuaError is a Lua error in flight. Every runtime type error and every
// error() call panics with a *LuaError; pcall (and lug.Run) recover it.
// Other panics (Go runtime errors in host code) are wrapped as string
// errors when recovered.
type LuaError struct {
	Value Value
}

func (e *LuaError) Error() string {
	if e.Value.kind == KindString {
		return e.Value.str
	}
	return "error: " + ToString(e.Value)
}

// Error raises a Lua error carrying v, like Lua's error(v).
func Error(v Value) { panic(&LuaError{Value: v}) }

// ErrorString raises a Lua error with a string message.
func ErrorString(msg string) { panic(&LuaError{Value: String(msg)}) }

// Errorf raises a Lua error with a formatted message.
func Errorf(format string, args ...any) {
	panic(&LuaError{Value: String(fmt.Sprintf(format, args...))})
}

// WrapPanic converts a recovered panic value into a *LuaError.
func WrapPanic(r any) *LuaError {
	if le, ok := r.(*LuaError); ok {
		return le
	}
	return &LuaError{Value: String(fmt.Sprint(r))}
}

// Protect runs f, converting any panic into a returned *LuaError.
func Protect(f func()) (err *LuaError) {
	defer func() {
		if r := recover(); r != nil {
			err = WrapPanic(r)
		}
	}()
	f()
	return nil
}
