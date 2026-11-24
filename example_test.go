package lug_test

import (
	"errors"
	"fmt"

	"github.com/pulseaiclub/lug"
)

// The package is a library: import it, make an environment, hand it a script.
// These examples are checked by `go test`, so the output shown is what the code
// actually produces.

func Example() {
	env := lug.New()
	lug.Bind(env, "greet", func(name string) string { return "hello, " + name })

	rets, err := lug.DoString(env, `return greet("gopher")`)
	if err != nil {
		panic(err)
	}
	fmt.Println(lug.Get[string](rets, 0))

	// Output: hello, gopher
}

func ExampleBind() {
	env := lug.New()

	// Any Go function can be bound. A final error return becomes a Lua error, so
	// it is catchable with pcall instead of killing the host.
	lug.Bind(env, "divide", func(a, b float64) (float64, error) {
		if b == 0 {
			return 0, errors.New("division by zero")
		}
		return a / b, nil
	})

	rets, err := lug.DoString(env, `
		local ok, msg = pcall(divide, 1, 0)
		return string.format("%.2f %s %s", divide(9, 2), tostring(ok), msg)
	`)
	if err != nil {
		panic(err)
	}
	fmt.Println(lug.Get[string](rets, 0))

	// Output: 4.50 false division by zero
}

type Character struct {
	Name string
	HP   float64
}

func (c *Character) Damage(n float64) {
	c.HP -= n
	if c.HP < 0 {
		c.HP = 0
	}
}

func ExampleExpose() {
	env := lug.New()
	c := &Character{Name: "gopher", HP: 100}

	// Expose keeps the value live: fields and methods are reachable from Lua and
	// changes are visible in Go, because it is the same object.
	lug.Expose(env, "player", c)

	if _, err := lug.DoString(env, `
		player:Damage(30)
		player.Name = player.Name .. "!"
	`); err != nil {
		panic(err)
	}

	fmt.Println(c.Name, c.HP)

	// Output: gopher! 70
}

func ExampleScript() {
	env := lug.New()

	// Compile once; run as often as needed, from many goroutines.
	s, err := lug.Compile(`local n = ...; return n * 2`, "double")
	if err != nil {
		panic(err) // syntax error
	}

	for _, n := range []float64{1, 2, 21} {
		v, err := lug.Run1[float64](env, s.Fn(), lug.Number(n))
		if err != nil {
			panic(err) // runtime error
		}
		fmt.Print(v, " ")
	}
	fmt.Println()

	// Output: 2 4 42
}
