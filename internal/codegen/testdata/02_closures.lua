-- closures capture variables by reference
local function counter()
    local n = 0
    return function()
        n = n + 1
        return n
    end
end
local c1, c2 = counter(), counter()
print(c1(), c1(), c2())

-- shared upvalue between two closures
local function pair()
    local v = 0
    local inc = function(d) v = v + d return v end
    local get = function() return v end
    return inc, get
end
local inc, get = pair()
inc(5)
inc(3)
print(get())

-- loop variables are fresh per iteration
local fs = {}
for i = 1, 3 do fs[i] = function() return i * 10 end end
print(fs[1](), fs[2](), fs[3]())

-- recursive local function
local function fib(n)
    if n < 2 then return n end
    return fib(n-1) + fib(n-2)
end
print(fib(10))

-- closures over while-loop body locals
local j = 0
local gs = {}
while j < 3 do
    j = j + 1
    local k = j * j
    gs[j] = function() return k end
end
print(gs[1](), gs[2](), gs[3]())

-- function defined with `local f = function` cannot see itself,
-- but local function can (recursion through upvalue)
local fact
fact = function(n) if n <= 1 then return 1 end return n * fact(n-1) end
print(fact(6))
