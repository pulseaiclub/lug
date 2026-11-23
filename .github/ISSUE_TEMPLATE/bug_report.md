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

```sh
lug repro.lua
```

### Environment

- lug version or commit:
- Go version (`go version`):
- OS and architecture:
- How the script is loaded: `lug script.lua` / `lug -e` / `lug.DoString` /
  `lug.DoFile` / `lug.CompileFile` / `load` inside Lua / sandbox

### Anything else

<!-- Panics, stack traces, and `go test ./...` output are all useful. -->
