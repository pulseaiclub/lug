// Command greet is a complete example of using lug as a library: transpile a
// Lua script to Go at build time, then load and run it from the host program.
//
// Build and run:
//
//	go generate ./examples/greet   # regenerates greet_lug.go from greet.lua
//	go run ./examples/greet
package main

//go:generate go run ../../cmd/lug gen -pkg main -o greet_lug.go greet.lua
//go:generate go run ../../cmd/lug gen -pkg main -o half_lug.go half.lua

import (
	"fmt"
	"os"

	"github.com/pulseaiclub/lug"
)

// Player is host state that Lua scripts can read, mutate and call methods on.
type Player struct {
	HP float64
}

// Damage is callable from Lua as player:Damage(25).
func (p *Player) Damage(dmg float64) float64 {
	p.HP -= dmg
	if p.HP < 0 {
		p.HP = 0
	}
	return p.HP
}

func main() {
	env := lug.New()
	player := &Player{HP: 100}
	lug.Expose(env, "player", player)                   // Go value  -> Lua userdata
	lug.Bind(env, "roll", func(sides float64) float64 { // Go func   -> Lua function
		return sides // a real script would roll a die here
	})

	// 1. Run the transpiled chunk once, with arguments like `lua greet.lua x`.
	rets, err := lug.Run(env, Chunk_greet, lug.String("gopher"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "lua error:", err)
		os.Exit(1)
	}
	// The chunk returned `who, player.HP, #who`.
	who, _ := lug.As[string](rets[0])
	hp, _ := lug.As[float64](rets[1])
	n, _ := lug.As[int](rets[2])
	fmt.Printf("chunk returned: who=%q hp=%v len=%d\n", who, hp, n)
	fmt.Printf("the Go struct behind the userdata now has HP=%v\n", player.HP)

	// 2. Call a Lua function the script defined, as often as needed.
	greet := env.Get("greet")
	for _, name := range []string{"ada", "grace"} {
		rets, err := lug.CallErr(env, greet, lug.String(name))
		if err != nil {
			fmt.Fprintln(os.Stderr, "lua error:", err)
			os.Exit(1)
		}
		fmt.Printf("greet(%q) returned %v\n", name, lug.Get[int](rets, 0))
	}

	// 3. Errors inside Lua arrive as a Go error, and the process keeps running.
	if _, err := lug.Run(env, Chunk_greet, lug.String("boom")); err != nil {
		fmt.Println("handled lua error:", err)
	}

	// 4. A chunk can also be run with a typed single result.
	half, err := lug.Run1[float64](env, Chunk_half, lug.Number(7))
	if err == nil {
		fmt.Println("half(7) =", half)
	}
}
