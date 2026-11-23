# lug

Lua embedded in Go as a **scripting language**: source is parsed and interpreted
at run time, so there is no build step, no generated code and no Go toolchain in
the loop. Drop a `.lua` file next to your binary and run it.

- **Lua 5.4 syntax** — closures, metatables, varargs, coroutines, `goto`,
  integer division, bitwise operators, `<const>` attributes.
- **Typed Go interop** — bind any Go function, expose any Go value; fields and
  methods stay reachable from Lua. Reflection happens once at bind time.
- **Sandboxing** — one environment per script, with `os`/`io`/`print` granted
  explicitly and `os.execute`, `os.getenv`, `load` and `require` never.
- **Zero dependencies** — standard library only.

```go
env := lug.New()
lug.Bind(env, "log", func(msg string) { fmt.Println(msg) })
lug.Expose(env, "player", &Player{HP: 100})

if _, err := lug.DoFile(env, "scripts/init.lua"); err != nil {
	log.Fatal(err) // syntax and runtime errors arrive as a Go error
}
```

## Install

```sh
go get github.com/pulseaiclub/lug
go install github.com/pulseaiclub/lug/cmd/lug@latest   # the `lug` command
```

## CLI

```
lug [options] script.lua [args...]
lug -e 'CODE' [args...]
lug version
```

The chunk receives the arguments as `...` and through the global `arg` table,
like the reference interpreter:

```sh
$ cat hello.lua
print("hello", ...)
print(arg[0], arg[1])
$ lug hello.lua world extra
hello	world	extra
hello.lua	world
```

## Embedding API

```go
env := lug.New()

// Go function -> Lua function (any signature; see runtime.WrapFunc).
lug.Bind(env, "damage", func(p *Player, dmg float64) float64 { return p.takeDamage(dmg) })

// Go value -> Lua userdata. Fields use their Go names, methods work with `:`.
lug.Expose(env, "player", &Player{HP: 100})

// Run code from a string, or from a file.
rets, err := lug.DoString(env, `return player.HP`)
rets, err = lug.DoFile(env, "scripts/init.lua")
```

Compile once, run often:

```go
s, err := lug.CompileFile("scripts/init.lua")
if err != nil {
	return err // syntax error
}
rets, err := s.Run(env, lug.String("arg")) // runtime error
```

A compiled script is an ordinary `lug.Fn`, so it can be stored, passed to
another script, registered in a table, or wrapped in a coroutine.

Calling Lua from Go:

```go
f := env.Get("answer")                     // a global the script defined
rets, err := lug.CallErr(env, f, lug.Number(5))
n := lug.Get[float64](rets, 0)              // panics on a shape mismatch
n, ok := lug.GetOK[float64](rets, 0)        // tolerant form
```

Results are read with generic helpers, so the destination type is written once:

```go
hp, err := lug.Run1[float64](env, Chunk_score)   // a chunk that returns one value
xs, err := lug.Slice[float64](lug.TableOf([]float64{1, 2}))
p, ok := lug.Userdata[Player](env.Get("player")) // unwrap Expose'd Go data
env.Set("count", lug.ValueOf(42))                // Go primitives -> Lua values
```

Supported `As[T]` / `Get[T]` types: `Value`, `float64`, `int64`, `int`, `string`,
`bool`, `*Table`. `Bind` and `Expose` stay `any`-typed on purpose: a Lua binding
may take any number of parameters of any types, and Go's type sets cannot express
"any function signature".

## Loading scripts from Lua

Importing `lug` links the interpreter, which also installs Lua's own loaders:

```lua
local chunk = load("return 6 * 7")     -- nil, message on a syntax error
print(chunk())                          --> 42
print(dofile("scripts/other.lua"))      -- runs a file, returns its results
print(loadfile("scripts/other.lua"))    -- compiles, does not run
```

`examples/embed` is a complete plugin host: it walks a directory, gives every
script its own sandbox, calls `on_load` / `on_tick` / `on_unload` hooks, and
keeps a broken script from affecting the others.

## Sandboxing

```go
env := lug.NewSandboxed("mod-name", lug.SandboxOptions{
	AllowPrint: true,
	OnPrint:    func(tag, line string) { fmt.Printf("[%s] %s\n", tag, line) },
})
```

A sandbox starts with no capabilities: no `os`, no `io`, no `load`/`dofile`, no
`require`/`debug`, no `print` unless asked for, and its own globals table.
`SandboxOptions{OS: true}` exposes only `os.time`, `os.date`, `os.clock` and
`os.difftime`; `IO: true` only `io.write`. `os.execute`, `os.remove`,
`os.getenv` and `os.exit` are never available.

The sandbox restricts capabilities, not resources: a script that loops forever
cannot be interrupted. Use a separate process for genuinely untrusted code.

## What is supported

The whole language: `if`/`while`/`repeat`/numeric and generic `for`, `goto` and
labels, `break`, multiple assignment and multiple returns, varargs, closures with
shared upvalues, table constructors with all field forms, method definitions and
calls, `and`/`or` returning operands, integer division `//`, bitwise
`& | ~ << >>`, and `<const>` (parsed and enforced as read-only).

Standard library: `print`, `type`, `tostring`, `tonumber`, `error`, `pcall`,
`xpcall`, `assert`, `select`, `next`, `pairs`, `ipairs`, `rawget`, `rawset`,
`rawequal`, `rawlen`, `setmetatable`, `getmetatable`, `collectgarbage`, `load`,
`loadfile`, `dofile`; `string` (including a full pattern matcher and `format`),
`table`, `math`, `os` (time/date/clock/difftime/getenv/exit/remove/rename),
`io.write`, `coroutine`.

### Numbers

Numbers are `float64` only, like Lua 5.1: `1/2` is `0.5`, `1//2` is `0.0`, and
`//`/`%`/bitwise operators follow Lua 5.4's float semantics. `string`↔`number`
coercion follows 5.4 (`"10" + 5` is `15`).

## Performance

Measured on an Apple M3, compared with gopher-lua v1.1.2 (a bytecode VM) doing
the same work:

| Workload | lug | gopher-lua |
|---|---|---|
| `fib(24)` (75k calls) | 32 ms | 8.5 ms |
| 100k numeric loop with `%` | 4.5 ms | 8.6 ms |

Arithmetic-heavy code is faster than a bytecode VM, because there is no
per-instruction dispatch table to walk. Call-heavy code is slower, because every
Lua call goes through the `Fn(env, []Value) []Value` ABI and allocates its
argument and result slices — the price of a uniform ABI that lets Go functions,
coroutines and `pcall` share one calling convention.

Benchmarks: `go test ./internal/interp -bench . -run xxx`.

## Layout

```
lug.go                 public embedding API (Bind, Expose, ValueOf, CallErr, ...)
script.go              Compile / CompileFile / DoString / DoFile, sandbox setup
generic.go             As / Get / Run1 / Slice / TableOf / Userdata helpers
cmd/lug                CLI: run a script, -e CODE
internal/lexer         Lua 5.4 lexer
internal/ast           surface AST
internal/parser        recursive-descent parser with precedence climbing
internal/interp        the interpreter: AST -> values
runtime/               values, tables, closures, metatables, stdlib, coroutines
examples/embed         run-time plugin host with per-script sandboxes
```

The interpreter and the runtime are separate on purpose: the runtime knows
nothing about the AST, and the interpreter only uses the exported runtime API. A
`Script` is just an `Fn`, which is why interpreted functions, Go functions and
library functions are interchangeable.

## Testing

```sh
go test ./...
go test ./internal/interp -bench . -run xxx
```

`internal/interp/testdata` holds scripts plus golden stdout (`.want`) or an
expected error message (`.wanterr`). They cover closures and shared upvalues,
metatables, varargs, multiple-assignment order, `goto`, coroutines, error
propagation, numeric/bitwise semantics and the standard library — including the
cases that are easy to get wrong, such as `i, t[i] = 2, 99` (which must write
`t[1]`) and closures capturing a loop variable.

## Limitations

- Numbers are `float64` only; there are no 64-bit integers.
- `coroutine.yield` cannot cross a Go stack frame: a Go function called from Lua
  cannot yield. Coroutines are goroutine-backed, so yielding through nested Lua
  calls works.
- No `require`, no `package` loader, no debug library. `load` accepts string
  chunks only, not reader functions.
- Error messages carry no line numbers.
- No `_ENV`/`setfenv` rebinding.

## License

MIT
