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
cmd/lug/               CLI (gen, run, version)
internal/lexer         Lua 5.4 lexer
internal/ast           surface AST
internal/parser        recursive-descent parser with precedence climbing
internal/codegen       Lua AST → Go source (specialization passes live here)
runtime/               the generated code's runtime: Value, Table, Env,
                       closures, metatables, stdlib, coroutines
internal/codegen/testdata  differential-style golden tests (Lua + expected output)
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

## Limitations

- Numbers are `float64` only (documented above); `string`↔`number` coercion
  follows Lua 5.4.
- `coroutine.yield` cannot cross a Go stack frame (calling a Go function that
  yields is unsupported), and coroutines are goroutine-backed.
- `io` is limited to `io.write`; `load`, `dofile`, `require` and the debug
  library are not implemented.
- The transpiler targets AOT compilation: Lua source is known at build time.
  There is no runtime `load` of new source.

## License

MIT.
