---
name: Bug report
about: Something behaves differently from the Lua 5.4 reference interpreter, or fails to work at all
title: ''
labels: bug
assignees: ''
---

### What happened

<!-- The actual output, including the exact whitespace. `print` separates its
arguments with a TAB and ends the line with "\n"; `io.write` writes no
separators. -->

```lua
-- the smallest script that shows the problem
```

### What you expected

<!-- If a real Lua 5.4 interpreter is available, paste its output here. -->

### Steps to reproduce

<!-- A Go test or a few lines of Go that call lug is ideal; the script alone is
fine too if the behaviour does not depend on the host. -->

```go
rets, err := lug.DoString(lug.New(), `return 1`)
```

### Environment

- lug version or commit:
- Go version (`go version`):
- OS and architecture:
- How the script is loaded: `lug.DoString` / `lug.DoFile` / `lug.CompileFile` +
  `Script.Run` / `load` inside Lua / inside a sandbox

### Anything else

<!-- Panics, stack traces, and `go test ./...` output are all useful. -->
