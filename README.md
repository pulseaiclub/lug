# lug

`lug` embeds **Lua 5.4** in Go by **transpiling Lua source into Go code** at build
time — no interpreter loop, no `LValue`/stack/registry API, no reflection on the
call path.

```lua
-- script.lua
local function fib(k)
    if k < 2 then return k end
    return fib(k - 1) + fib(k - 2)
end
return fib(30)
```

```sh
lug gen -pkg game -o script_lug.go script.lua
```

generates plain Go you can read, step through and profile:

```go
var fib_2 func(*lg.Env, float64) float64
fib_2 = func(env *lg.Env, k_3 float64) float64 {
	if k_3 < (2.0) {
		return k_3
	}
	return (fib_2(env, (k_3 - (1.0))) + fib_2(env, (k_3 - (2.0))))
}
```

Because the transpiler proves `fib` numeric, its call is a direct Go call with
`float64` arguments: it runs within ~1.2x of the hand-written Go equivalent.

## Install

```sh
go install github.com/pulseaiclub/lug/cmd/lug@latest
```

## CLI

```
lug gen [-pkg name] [-name Chunk_main] [-o out.go] file.lua   # transpile
lug run file.lua [args...]                                     # transpile + go run
lug version
```

`lug run` builds a throwaway module next to your script (with a `replace` to
the local checkout when the environment variable `LUG_SRC` points at it), which
makes scripted experiments painless:

```sh
lug run bench.lua
```

Typical project wiring:

```go
//go:generate lug gen -pkg game -o script_lug.go scripts/main.lua
```

## Loading a script and running it

Compilation happens at build time, so "loading a script" is two steps: generate
Go from the Lua source once, then call the generated chunk like any other
function. `examples/greet` is the complete runnable version of this section:

```sh
go generate ./examples/greet   # greet.lua + half.lua -> *_lug.go
go run ./examples/greet
```

The script (`examples/greet/greet.lua`):

```lua
local args = { ... }
local who = args[1] or "world"

print("hello, " .. who)
local before = player.HP       -- exposed Go struct, exact Go field names
player:Damage(25)              -- pointer-receiver method
print("HP:", before, "->", player.HP)

function greet(name)           -- host calls this later, as often as it wants
	print("greet:", name)
	return #name
end

return who, player.HP, #who    -- several values back to Go
```

The host (`examples/greet/main.go`):

```go
type Player struct{ HP float64 }

func (p *Player) Damage(dmg float64) float64 { p.HP -= dmg; return p.HP }

func main() {
	env := lug.New()
	player := &Player{HP: 100}
	lug.Expose(env, "player", player)                       // Go value -> userdata
	lug.Bind(env, "roll", func(sides float64) float64 { return sides })

	// 1. Run the chunk once, with arguments like `lua greet.lua gopher`.
	rets, err := lug.Run(env, Chunk_greet, lug.String("gopher"))
	if err != nil {
		log.Fatal(err) // Lua errors arrive as a Go error
	}
	who, _ := lug.As[string](rets[0])
	hp, _ := lug.As[float64](rets[1])
	fmt.Println(who, hp, player.HP) // gopher 75 75

	// 2. Call a Lua function the script defined.
	greet := env.Get("greet")
	n, err := lug.CallErr(env, greet, lug.String("ada")) // n[0] == 3

	// 3. Errors inside Lua are values, not crashes.
	_, err = lug.Run(env, Chunk_greet, lug.String("boom")) // "boom was requested"

	// 4. A single-result chunk with a typed result.
	half, err := lug.Run1[float64](env, Chunk_half, lug.Number(7)) // 3.5
}
```

Output:

```
hello, gopher
HP:	100	->	75
greet:	ada
chunk returned: who="gopher" hp=75 len=6
handled lua error: boom was requested
half(7) = 3.5
```

Notes:

- The chunk is an ordinary `lug.Fn` value, so it can be stored in a map,
  passed to a goroutine, or registered in a plugin table.
- Fields on exposed structs use their **Go names** (`player.HP`, not `hp`), and
  methods work with either call style: `player:Damage(25)` or
  `player.Damage(25)`.
- A script cannot be loaded from a string at runtime: `load`/`dofile` are not
  implemented, by design. The source must exist at build time.

## Running scripts at run time

The transpiler needs the source at build time. When scripts appear later —
user mods, config snippets, hot reloads — use the interpreter instead:

```go
env := lug.New()
lug.Bind(env, "log", func(msg string) { fmt.Println(msg) })

// Compile once, run as often as needed.
s, err := lug.CompileFile("scripts/hello.lua")
if err != nil {
	return err // syntax error, reported as a Go error
}
rets, err := s.Run(env, lug.String("world"))

// Or straight from a string.
if _, err := lug.DoString(env, `log("hi " .. name)`); err != nil {
	return err
}
```

A compiled script is an ordinary `lug.Fn`, so interpreted and transpiled code
are interchangeable: `pcall`, coroutines, metatables, `Bind`, `Expose` and the
whole standard library apply to both, and they can call each other.

Because importing `lug` links the interpreter, Lua code gains the loaders:

```lua
local chunk = load("return 6 * 7")     -- nil, message on a syntax error
print(chunk())                          --> 42
print(dofile("scripts/other.lua"))      -- runs a file, returns its results
```

`examples/embed` is the full version: it walks a directory, gives every script
its own sandbox, calls `on_load` / `on_tick` / `on_unload` hooks, and keeps a
broken script from affecting the others.

### Sandboxing scripts

```go
env := lug.NewSandboxed("mod-name", lug.SandboxOptions{
	AllowPrint: true,
	OnPrint:    func(tag, line string) { fmt.Printf("[%s] %s\n", tag, line) },
})
```

A sandbox starts empty of capabilities: no `os`, no `io`, no `load`/`dofile`,
no `require`/`debug`, and no `print` unless asked for. Each sandbox has its own
globals table, so two scripts never share variables. `SandboxOptions{OS: true}`
exposes only `os.time`, `os.date`, `os.clock`, `os.difftime`; `IO: true` only
`io.write`. `os.execute`, `os.remove`, `os.getenv` and `os.exit` are never
available.

The sandbox limits capabilities, not resources: a script that loops forever
cannot be interrupted, because transpiled loops are plain Go loops. Use a
separate process for genuinely untrusted code.

### Cost

Interpreted code exists for scripts the binary did not ship with. Measured on
this machine (Apple M3):

| Workload | Transpiled | Interpreted | gopher-lua |
|---|---|---|---|
| `fib(24)` | 0.14 ms | 34 ms | 8.5 ms |
| 100k numeric loop | 0.08 ms | 14 ms | 8.6 ms |

So the interpreter is ~200x slower than the transpiler and ~2-4x slower than
gopher-lua's bytecode VM: every node goes through an interface type switch,
every value is a 48-byte box, and every call allocates a scope and an argument
slice. Keep the hot path transpiled — that is what the AOT half is for — and
interpret the layer that must load at run time.

## Embedding API

```go
env := lug.New()
lug.Expose(env, "player", &Player{HP: 100})               // struct → Lua value
lug.Bind(env, "damage", func(p *Player, d float64) float64 { // Go func → Lua function
	return p.takeDamage(d)
})

rets, err := lug.Run(env, gameChunk, lug.Number(3))
```

`Run` returns a Go `error` (Lua errors are captured, not panics) and `Call`
invokes any Lua function value. `reflect` is used only while binding, never on
the call path.

Results are read with generic helpers, so the destination type is written once:

```go
hp, err := lug.Run1[float64](env, gameChunk, lug.Number(3)) // chunk returns one value
score := lug.Get[int](rets, 0)                              // panics on shape mismatch
best, ok := lug.GetOK[float64](rets, 1)                     // tolerant form
xs, err := lug.Slice[float64](lug.TableOf([]float64{1, 2})) // table <-> slice
p, ok := lug.Userdata[Player](env.Get("player"))            // unwrap Expose'd Go data
env.Set("count", lug.ValueOf(42))                           // primitives map to Lua kinds
```

Supported `As[T]` / `Get[T]` types: `Value`, `float64`, `int64`, `int`,
`string`, `bool`, `*Table`. The older `AsFloat` / `AsInt` / `AsString` /
`AsBool` / `AsTable` remain available.

`Bind` and `Expose` stay `any`-typed on purpose: a Lua binding may take any
number of parameters of any types, and Go's type sets cannot express "any
function signature". Reflection happens once, at bind time.

## What is supported

- Full Lua 5.4 syntax: `goto`/labels, `<const>`, multiple returns and
  assignment, varargs, method definitions, long strings/comments, hex floats,
  all string escapes (`\x`, `\u{}`, `\z`, `\ddd`).
- Closures with real upvalues, metatables (`__index`, `__newindex`, `__call`,
  `__eq`, `__lt`, `__le`, `__concat`, `__len`, `__tostring`, `__close` excluded),
  `pcall`/`xpcall`, error values, coroutines (`coroutine.create/wrap/resume/yield/
  status`).
- Standard library subset: `print`, `type`, `tostring`, `tonumber`, `pairs`,
  `ipairs`, `next`, `select`, `error`, `assert`, `rawget/rawset/rawequal/rawlen`,
  `setmetatable/getmetatable`, `unpack`, `string.*` (including the complete
  Lua pattern matcher, `format`, `gmatch`, `gsub`), `table.*`, `math.*`, `os.*`
  and `io.write`.
- Lua 5.4 statement-boundary rules are reproduced exactly: newlines are
  whitespace, so `a = b\n(c).d` parses as `a = b(c).d`, like the reference
  implementation.

### Numbers

Parsing and semantics follow Lua 5.4, but **all numbers are `float64`**
(Lua 5.1 numeric model). `math.maxinteger`/bitwise operators still work through
exact integer conversion; there is no separate integer subtype, so
`7 // 2 == 3.0` and `print(1)` prints `1` as before, while `1 == 1.0` is true.

### Optimizations performed by the transpiler

| Optimization | Effect |
| --- | --- |
| Unique renaming of every local (`name_N`) | shadowing, closures and `goto` hoisting are trivially correct |
| Direct-call specialization | a local function only ever called directly becomes `func(*lg.Env, a, b lg.Value) lg.Value` — no argument slice allocation per call |
| Numeric specialization | locals, parameters, results and `for` control variables proven to be numbers become Go `float64`; arithmetic and comparisons compile to native operators |
| Value representation | 48-byte tagged union (`kind`, `num`, `str`, `ptr`) instead of `any`; numbers and booleans never allocate |
| Tables | array part + string/number/other hash parts, insertion order preserved so `pairs` is deterministic |

### Benchmarks

`go test ./internal/codegen/bench -bench .` (Apple M3, Go 1.26):

| Benchmark | Transpiled Lua | Hand-written Go | Ratio |
| --- | --- | --- | --- |
| `fib(24)`, recursive | 212 µs | 176 µs | 1.20x |
| 100k-iteration numeric loop | 95 µs | 70 µs | 1.36x |

Without the specializations (pure `Value` code, e.g. anything that cannot be
proven monomorphic) the same benchmarks are ~20–40x slower than Go but still
several times faster than a tree-walking interpreter, because there is no
dispatch loop and no per-instruction decoding.

## Layout

```
lug.go                 public embedding API
script.go              run-time compilation (Script, DoString, DoFile, sandbox)
generic.go             As/Get/Run1/Slice/TableOf/Userdata helpers
cmd/lug/               CLI (gen, run, version)
internal/lexer         Lua 5.4 lexer
internal/ast           surface AST
internal/parser        recursive-descent parser with precedence climbing
internal/codegen       Lua AST → Go source (specialization passes live here)
internal/interp        Lua AST interpreter, for scripts unknown at build time
runtime/               the generated code's runtime: Value, Table, Env,
                       closures, metatables, stdlib, coroutines, sandbox
examples/greet         build-time transpiling, end to end
examples/embed         run-time loading of scripts from a directory
internal/codegen/testdata  golden scripts run by *both* the transpiler and
                       the interpreter, so semantics cannot drift apart
```

The code generator runs three analyses before emitting: usage collection
(which locals are only called), numeric type inference (to a fixpoint), and
then a final emission pass that applies both. `internal/codegen/bench` holds
committed samples of generated code so the performance claims stay honest.

## Testing

```sh
go test ./...                                   # unit + end-to-end golden tests
go test ./internal/codegen -run TestE2E -v       # transpile, compile, run, diff
go test ./internal/codegen/bench -bench .
```

Each `internal/codegen/testdata/*.lua` file is transpiled, compiled in a
temporary module against the real runtime, executed, and its stdout compared
with the matching `.want` file (or `.wanterr` for expected failures). Adding a
case is just dropping in two files.

The same scripts are then interpreted and compared with the *same* golden files
(`internal/interp`). Because one source file is checked on both execution
paths, the interpreter cannot drift from the transpiled semantics — that is how
the multiple-assignment ordering bug (`i, t[i] = 2, 99`) and the userdata
method-receiver bug were found.

## Limitations

- Numbers are `float64` only (documented above); `string`↔`number` coercion
  follows Lua 5.4.
- `coroutine.yield` cannot cross a Go stack frame (calling a Go function that
  yields is unsupported), and coroutines are goroutine-backed.
- `io` is limited to `io.write`; `require` and the debug library are not
  implemented.
- The transpiler targets AOT compilation. Scripts that only exist at run time
  go through the interpreter (see above), which is ~200x slower; `load` is
  available only because importing `lug` links that interpreter, and it accepts
  string chunks only (no reader functions).
- The interpreter has no `setfenv`/`_ENV` rebinding, and its error messages
  carry no line numbers.

## License

MIT.
