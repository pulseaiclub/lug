package lug_test

import (
	"testing"

	"github.com/pulseaiclub/lug"
	lg "github.com/pulseaiclub/lug/runtime"
)

type Player struct {
	Name string
	HP   float64
}

func (p *Player) Damage(d float64) float64 {
	p.HP -= d
	return p.HP
}

// chunk emulates transpiled code: a function that returns fixed values.
func chunk(rets ...lg.Value) lug.Fn {
	return func(_ *lg.Env, _ []lg.Value) []lg.Value { return rets }
}

func TestGenericAs(t *testing.T) {
	if v, ok := lug.As[float64](lug.Number(3.5)); !ok || v != 3.5 {
		t.Fatalf("As[float64] = %v, %v", v, ok)
	}
	if v, ok := lug.As[string](lug.String("hi")); !ok || v != "hi" {
		t.Fatalf("As[string] = %v, %v", v, ok)
	}
	if v, ok := lug.As[int64](lug.Number(42)); !ok || v != 42 {
		t.Fatalf("As[int64] = %v, %v", v, ok)
	}
	if v, ok := lug.As[int](lug.Number(7)); !ok || v != 7 {
		t.Fatalf("As[int] = %v, %v", v, ok)
	}
	if v, ok := lug.As[bool](lug.True); !ok || !v {
		t.Fatalf("As[bool] = %v, %v", v, ok)
	}
	if _, ok := lug.As[bool](lug.Number(1)); ok {
		t.Fatal("As[bool] of a number must fail")
	}
	if _, ok := lug.As[float64](lug.Nil); ok {
		t.Fatal("As[float64] of nil must fail")
	}
	if v, ok := lug.As[lg.Value](lug.Nil); !ok || !v.IsNil() {
		t.Fatalf("As[Value] = %v, %v", v, ok)
	}
}

func TestGenericGet(t *testing.T) {
	rets := []lg.Value{lug.String("a"), lug.Number(2)}

	if got := lug.Get[string](rets, 0); got != "a" {
		t.Fatalf("Get[string] = %q", got)
	}
	if got := lug.Get[int](rets, 1); got != 2 {
		t.Fatalf("Get[int] = %d", got)
	}
	if _, ok := lug.GetOK[float64](rets, 0); ok {
		t.Fatal("GetOK[float64] on a string must fail")
	}
	if _, ok := lug.GetOK[float64](rets, 5); ok {
		t.Fatal("GetOK on an out-of-range index must fail")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Get must panic on a type mismatch")
			}
		}()
		lug.Get[bool](rets, 0)
	}()
}

func TestGenericRun1(t *testing.T) {
	env := lug.New()

	n, err := lug.Run1[float64](env, chunk(lug.Number(6.5)))
	if err != nil || n != 6.5 {
		t.Fatalf("Run1 = %v, %v", n, err)
	}

	s, err := lug.Run1[string](env, chunk(lug.String("ok")))
	if err != nil || s != "ok" {
		t.Fatalf("Run1 = %v, %v", s, err)
	}

	// A Lua error surfaces as a Go error.
	boom := func(e *lg.Env, _ []lg.Value) []lg.Value {
		lg.Call(e, e.Get("error"), []lg.Value{lug.String("boom")})
		return nil
	}
	if _, err := lug.Run1[float64](env, boom); err == nil {
		t.Fatal("Run1 must return the Lua error")
	}

	// A type mismatch is a Go error, not a panic.
	if _, err := lug.Run1[float64](env, chunk(lug.String("nope"))); err == nil {
		t.Fatal("Run1 must report a type mismatch")
	}

	// No values at all.
	if _, err := lug.Run1[float64](env, chunk()); err == nil {
		t.Fatal("Run1 must report a missing result")
	}
}

func TestGenericUserdata(t *testing.T) {
	env := lug.New()
	p := &Player{Name: "ada", HP: 100}
	lug.Expose(env, "player", p)

	got, ok := lug.Userdata[Player](env.Get("player"))
	if !ok {
		t.Fatal("Userdata[Player] failed")
	}
	if got != p {
		t.Fatalf("Userdata returned a different pointer: %p vs %p", got, p)
	}
	if got.HP != 100 || got.Name != "ada" {
		t.Fatalf("unexpected value: %+v", *got)
	}

	if _, ok := lug.Userdata[string](env.Get("player")); ok {
		t.Fatal("Userdata[string] of a Player must fail")
	}
	if _, ok := lug.Userdata[Player](lug.Number(1)); ok {
		t.Fatal("Userdata of a number must fail")
	}
}

// TestBindCallRoundTrip checks that a bound Go function can be called from a
// chunk and the result read back with the generic helpers.
func TestBindCallRoundTrip(t *testing.T) {
	env := lug.New()
	lug.Bind(env, "damage", func(p *Player, d float64) float64 { return p.Damage(d) })

	p := &Player{HP: 100}
	lug.Expose(env, "player", p)

	// Emulates transpiled Lua: damage(player, 25).
	callDamage := func(e *lg.Env, _ []lg.Value) []lg.Value {
		f := e.Get("damage")
		return lg.Call(e, f, []lg.Value{e.Get("player"), lug.Number(25)})
	}
	hp, err := lug.Run1[float64](env, callDamage)
	if err != nil {
		t.Fatalf("Run1: %v", err)
	}
	if hp != 75 {
		t.Fatalf("hp = %v, want 75", hp)
	}
	if p.HP != 75 {
		t.Fatalf("the Go value behind the userdata was not mutated: %v", p.HP)
	}
}

func TestGenericSliceAndTableOf(t *testing.T) {
	src := []float64{1.5, 2.5, 3}
	tbl := lug.TableOf(src)
	if tbl.Len() != 3 {
		t.Fatalf("TableOf produced %d entries", tbl.Len())
	}
	got, err := lug.Slice[float64](tbl)
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}
	if len(got) != len(src) {
		t.Fatalf("Slice length = %d, want %d", len(got), len(src))
	}
	for i := range src {
		if got[i] != src[i] {
			t.Fatalf("Slice[%d] = %v, want %v", i, got[i], src[i])
		}
	}

	// A type mismatch is reported, not silently zeroed.
	if _, err := lug.Slice[string](tbl); err == nil {
		t.Fatal("Slice[string] of numbers must fail")
	}
	if _, err := lug.Slice[float64](nil); err == nil {
		t.Fatal("Slice of a nil table must fail")
	}
}

func TestValueOf(t *testing.T) {
	env := lug.New()
	env.Set("n", lug.ValueOf(42))
	if f, _ := lug.AsFloat(env.Get("n")); f != 42 {
		t.Fatalf("ValueOf(42) = %v", f)
	}
	env.Set("s", lug.ValueOf("hi"))
	if s, _ := lug.AsString(env.Get("s")); s != "hi" {
		t.Fatalf("ValueOf(\"hi\") = %q", s)
	}
	env.Set("b", lug.ValueOf(true))
	if b, _ := lug.AsBool(env.Get("b")); !b {
		t.Fatal("ValueOf(true) is not true")
	}
	// Structs still become userdata.
	env.Set("p", lug.ValueOf(&Player{HP: 5}))
	if p, ok := lug.Userdata[Player](env.Get("p")); !ok || p.HP != 5 {
		t.Fatalf("ValueOf(&Player) = %+v, %v", p, ok)
	}
}
