---
name: Feature request
about: Suggest a language feature, a library function, or an API improvement
title: ''
labels: enhancement
assignees: ''
---

### What you are trying to do

<!-- The Lua code you wish worked, and what the Go side looks like. -->

```lua
```

### Why the current API does not cover it

<!-- lug aims to stay a small runtime, so "Lua itself can do this in a helper
function" is a good answer; please say why that does not apply. -->

### Scope check

Which of these is closest?

- [ ] Lua 5.4 semantics that lug gets wrong or does not implement
- [ ] A standard library function that is missing
- [ ] The embedding API (`Bind`, `Expose`, `Compile`, ...)
- [ ] The `lug` command
- [ ] Performance

<!-- Known gaps, so you can say whether your request is one of them: numbers are
float64 only, coroutine.yield cannot cross a Go frame, there is no require or
package loader and no debug library, error messages carry no line numbers, and
_ENV/setfenv rebinding is unsupported. -->
