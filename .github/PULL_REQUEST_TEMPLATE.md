# What this changes

<!-- One or two sentences. If it fixes a bug, say what the wrong behaviour was. -->

## Checklist

- [ ] `go test ./...` passes
- [ ] `go test -race ./...` passes
- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...` passes
- [ ] A test covers the change (a Lua script in `internal/interp/testdata` with
      a `.want` file, or a Go test)
- [ ] If a public API changed, `example_test.go` still compiles and `go test
      -run Example .` passes
- [ ] If this changes performance, the numbers are in the description
      (`go test ./internal/interp -bench . -run xxx`)

## Notes for the reviewer

<!-- Anything that is not obvious from the diff: an ordering rule in the
interpreter, a Lua 5.4 behaviour that looks like a bug but is not, a case you
decided not to support. -->
