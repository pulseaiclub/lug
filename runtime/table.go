package runtime

import "math"

// Table is a Lua table: a dense 1-based array part plus hash parts keyed by
// type. Splitting the hash part by key type keeps the hot paths (string keys
// like t.name, integer keys like t[i]) allocation-free and map-overhead low.
//
// The hash part keeps insertion order so pairs() iterates deterministically
// (Lua leaves the order unspecified; being deterministic makes transpiled
// programs reproducible and testable).
type Table struct {
	arr    []Value // dense array part, index 0 == Lua index 1
	strs   map[string]Value
	nums   map[float64]Value
	misc   map[any]Value // bool, *Table, *Closure, *Userdata, *Coroutine keys
	order  []Value       // hash keys in first-insertion order
	ordPos map[any]int   // ordKey -> index in order
	meta   *Table
}

// NewTable creates an empty table.
func NewTable() *Table { return &Table{} }

// Metatable returns the table's metatable, or nil.
func (t *Table) Metatable() *Table { return t.meta }

// SetMetatable sets the table's metatable (nil to clear).
func (t *Table) SetMetatable(m *Table) { t.meta = m }

// ordKey is the comparable map key used for order bookkeeping.
func ordKey(k Value) any {
	switch k.kind {
	case KindString:
		return k.str
	case KindNumber:
		return k.num
	case KindBool:
		return k.num != 0
	default:
		return k.ptr
	}
}

func arrIndex(f float64, n int) (int, bool) {
	if f >= 1 && f <= float64(n) && f == math.Trunc(f) {
		return int(f) - 1, true
	}
	return 0, false
}

// hashKey is the map key for the misc part. Only used for non-string,
// non-number keys.
func hashKey(k Value) any {
	if k.kind == KindBool {
		return k.num != 0
	}
	return k.ptr
}

func (t *Table) addToOrder(k Value) {
	ok := ordKey(k)
	if t.ordPos == nil {
		t.ordPos = make(map[any]int)
	}
	if _, exists := t.ordPos[ok]; exists {
		return
	}
	t.ordPos[ok] = len(t.order)
	t.order = append(t.order, k)
}

// Get returns t[k] without invoking metamethods. Missing keys return nil.
func (t *Table) Get(k Value) Value {
	switch k.kind {
	case KindNil:
		return Nil
	case KindString:
		if t.strs != nil {
			return t.strs[k.str]
		}
	case KindNumber:
		if idx, ok := arrIndex(k.num, len(t.arr)); ok {
			return t.arr[idx]
		}
		if t.nums != nil {
			return t.nums[k.num]
		}
	default:
		if t.misc != nil {
			return t.misc[hashKey(k)]
		}
	}
	return Nil
}

// Set performs t[k] = v. Setting a key to nil removes it. nil and NaN keys
// raise errors, like Lua.
func (t *Table) Set(k, v Value) {
	if k.kind == KindNil {
		ErrorString("table index is nil")
	}
	if k.kind == KindNumber && math.IsNaN(k.num) {
		ErrorString("table index is NaN")
	}
	switch k.kind {
	case KindString:
		if v.IsNil() {
			if t.strs != nil {
				delete(t.strs, k.str)
			}
			return
		}
		if t.strs == nil {
			t.strs = make(map[string]Value)
		}
		if _, exists := t.strs[k.str]; !exists {
			t.addToOrder(k)
		}
		t.strs[k.str] = v
	case KindNumber:
		i := int(k.num)
		if k.num == float64(i) && i >= 1 && i <= len(t.arr)+1 {
			if v.IsNil() {
				if i <= len(t.arr) {
					t.arr[i-1] = Nil
					for len(t.arr) > 0 && t.arr[len(t.arr)-1].IsNil() {
						t.arr = t.arr[:len(t.arr)-1]
					}
				}
				return
			}
			if i == len(t.arr)+1 {
				// The key may have lived in nums while the array part grew
				// up to it; drop the stale entry.
				if t.nums != nil {
					delete(t.nums, k.num)
				}
				t.arr = append(t.arr, v)
				return
			}
			t.arr[i-1] = v
			return
		}
		if v.IsNil() {
			if t.nums != nil {
				delete(t.nums, k.num)
			}
			return
		}
		if t.nums == nil {
			t.nums = make(map[float64]Value)
		}
		if _, exists := t.nums[k.num]; !exists {
			t.addToOrder(k)
		}
		t.nums[k.num] = v
	default:
		if v.IsNil() {
			if t.misc != nil {
				delete(t.misc, hashKey(k))
			}
			return
		}
		if t.misc == nil {
			t.misc = make(map[any]Value)
		}
		hk := hashKey(k)
		if _, exists := t.misc[hk]; !exists {
			t.addToOrder(k)
		}
		t.misc[hk] = v
	}
}

// Len returns a valid border of the table (the length of the dense array
// part; the array part never contains nil holes at its tail).
func (t *Table) Len() int { return len(t.arr) }

// Next implements the pairs iteration protocol: given the previous key it
// returns the next key/value pair. prev == Nil starts iteration.
func (t *Table) Next(prev Value) (Value, Value, bool) {
	arrFrom := 0
	if !prev.IsNil() {
		if prev.kind == KindNumber && prev.num == math.Trunc(prev.num) && prev.num >= 1 && prev.num <= float64(len(t.arr)) {
			arrFrom = int(prev.num)
		} else {
			arrFrom = len(t.arr)
		}
	}
	for i := arrFrom; i < len(t.arr); i++ {
		if !t.arr[i].IsNil() {
			return Number(float64(i + 1)), t.arr[i], true
		}
	}
	start := t.orderPos(prev) + 1
	for i := start; i < len(t.order); i++ {
		k := t.order[i]
		if v := t.Get(k); !v.IsNil() {
			return k, v, true
		}
	}
	return Nil, Nil, false
}

func (t *Table) orderPos(k Value) int {
	if t.ordPos == nil {
		return -1
	}
	if i, ok := t.ordPos[ordKey(k)]; ok {
		return i
	}
	return -1
}

// Userdata wraps a Go value for Lua. Field and method access goes through
// reflection (see reflect.go); a metatable can override it.
type Userdata struct {
	Data  any
	meta  *Table
	cache map[string]Value // cached method wrappers
}

// NewUserdata wraps a Go value.
func NewUserdata(data any) *Userdata { return &Userdata{Data: data} }

// Metatable returns the userdata's metatable, or nil.
func (u *Userdata) Metatable() *Table { return u.meta }

// SetMetatable sets the userdata's metatable.
func (u *Userdata) SetMetatable(m *Table) { u.meta = m }

// UserdataVal returns a userdata Value wrapping u.
func UserdataVal(u *Userdata) Value { return Value{kind: KindUserdata, ptr: u} }
