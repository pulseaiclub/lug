-- goto: the classic continue idiom
local sum = 0
for i = 1, 10 do
    if i % 2 == 0 then goto continue end
    sum = sum + i
    ::continue::
end
print("odd sum", sum)

-- goto backwards, and goto out of a nested block
local n = 0
::again::
n = n + 1
if n < 3 then goto again end
print("goto loop", n)

do
    local i = 0
    ::top::
    i = i + 1
    print("nested", i)
    if i < 2 then goto top end
end

-- repeat: the condition sees the body's locals
local k = 0
repeat
    local double = k * 2
    k = k + 1
until double >= 4
print("repeat", k)

-- integer division, modulo, bitwise ops
print(7 // 2, 7 % 3, -7 // 2, 2^10)
print(5 & 3, 5 | 3, 5 ~ 3, 1 << 4, 32 >> 2, ~0)

-- metatables with a function __index and __call
local base = {greet = function(self) return "hi " .. self.name end}
local obj = setmetatable({name = "bob"}, {
    __index = function(t, key) return base[key] end,
    __call = function(self, x) return x * 2 end,
    __tostring = function(self) return "obj(" .. self.name .. ")" end,
})
print(obj:greet(), obj(21), tostring(obj), obj.missing)

-- varargs through nested functions
local function outer(...)
    local function inner(...) return select('#', ...) end
    return inner(...), ...
end
print(outer(1, 2, 3))

-- table constructor expansion rules
local function three() return 1, 2, 3 end
local a = {three()}
local b = {three(), three()}
local c = {x = three()}
print(#a, #b, c.x)

-- multiple assignment evaluates targets first, then values
local t = {1, 2}
local i = 1
i, t[i] = 2, 99
print(i, t[1], t[2])

-- and/or return operands, comparison is structural-free
print(nil or "fallback", false and "never", 1 and 2 or 3)
print(1 == 1.0, "1" == 1, "10" + 5, "abc" .. 1)

-- closures share one upvalue
local function counter()
    local c = 0
    return function() c = c + 1 return c end, function() return c end
end
local inc, get = counter()
inc(); inc()
print("shared upvalue", get())

-- numeric for with float step and a negative step
local acc = 0
for x = 1, 2, 0.25 do acc = acc + x end
for x = 3, 1, -1 do acc = acc + x end
print("for steps", acc)
