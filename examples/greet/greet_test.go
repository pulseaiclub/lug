package main

import (
	"testing"

	"github.com/pulseaiclub/lug"
)

// TestGreetChunk keeps the documented example honest: the chunk runs, the
// exposed Go value is mutated through a method call, and the results have the
// documented types.
func TestGreetChunk(t *testing.T) {
	env := lug.New()
	player := &Player{HP: 100}
	lug.Expose(env, "player", player)
	lug.Bind(env, "roll", func(sides float64) float64 { return sides })

	rets, err := lug.Run(env, Chunk_greet, lug.String("gopher"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rets) != 3 {
		t.Fatalf("chunk returned %d values, want 3", len(rets))
	}
	if who, _ := lug.As[string](rets[0]); who != "gopher" {
		t.Fatalf("who = %q, want gopher", who)
	}
	if hp, _ := lug.As[float64](rets[1]); hp != 75 {
		t.Fatalf("hp = %v, want 75", hp)
	}
	if player.HP != 75 {
		t.Fatalf("player.HP = %v, want 75", player.HP)
	}

	// The chunk defined a global the host can call later.
	rets, err = lug.CallErr(env, env.Get("greet"), lug.String("ada"))
	if err != nil {
		t.Fatalf("greet: %v", err)
	}
	if n, _ := lug.As[int](rets[0]); n != 3 {
		t.Fatalf("greet(\"ada\") = %v, want 3", n)
	}

	// Lua errors surface as a Go error.
	if _, err := lug.Run(env, Chunk_greet, lug.String("boom")); err == nil {
		t.Fatal("expected an error for the boom argument")
	}

	// Single-result chunk through Run1.
	half, err := lug.Run1[float64](env, Chunk_half, lug.Number(7))
	if err != nil || half != 3.5 {
		t.Fatalf("Run1[float64](half) = %v, %v", half, err)
	}
}
