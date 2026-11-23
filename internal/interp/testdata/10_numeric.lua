-- numeric specialization must not change Lua semantics
local x = 10
local y = x / 4
print(x, y, x % 3, x // 3, x ^ 2, -x)

local n = 0
for i = 1, 10 do n = n + i * 2 end
print(n)

for i = 1, 3 do print(i / 2, i % 2, i ^ 3) end

-- a local that is later assigned a non-number stays a Value
local s = 1
print(s)
s = "now a string"
print(s)

-- string coercion in arithmetic still works
local v = 1
v = v + "1"
print(v)

-- a local that becomes nil still works
local m = 1
print(m)
m = nil
print(m)

-- numeric locals as table keys and in concatenation
local k = 3
local t = {}
t[k] = "three"
print(t[3], "k=" .. k)

-- captured by a closure
local cnt = 0
local function bump() cnt = cnt + 1 return cnt end
bump()
bump()
print(cnt)

-- numeric parameters inferred from literal arguments
local function area(w, h) return w * h end
print(area(3, 4), area(2.5, 4))

-- a function taking strings is not specialized; numeric strings coerce
local function twice(a) return a + a end
print(twice(21), twice("21"))

print(7 // 2, 2 ^ 10, 10 / 2)

-- numeric multiple assignment must not observe partial updates
local function gcd(a, b) while b ~= 0 do a, b = b, a % b end return a end
print(gcd(1071, 462))

local p, q = 1, 2
p, q = q, p
print(p, q)
