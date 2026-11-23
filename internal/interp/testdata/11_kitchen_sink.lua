-- sorting with comparators
local t = {5, 3, 8, 1, 9, 2}
table.sort(t)
print(table.concat(t, ","))
table.sort(t, function(a, b) return a > b end)
print(table.concat(t, ","))

-- class inheritance
local Base = {}
Base.__index = Base
function Base.new(name) return setmetatable({name = name}, Base) end
function Base:describe() return "Base:" .. self.name end
local Derived = setmetatable({}, {__index = Base})
Derived.__index = Derived
function Derived.new(name, level)
    local o = Base.new(name)
    o.level = level
    return setmetatable(o, Derived)
end
Derived.describe = function(self) return "Derived:" .. self.name .. "@" .. self.level end
local d = Derived.new("dave", 3)
print(d:describe(), getmetatable(d) == Derived, d.describe == Derived.describe)

-- patterns
print(("hello world"):gsub("(%w+)", function(w) return w:upper() end))
print(("2024-06-01"):match("(%d+)-(%d+)-(%d+)"))
print(("a,b,,c"):gsub(",", ";"))
for w in ("the quick brown fox"):gmatch("%a+") do io.write(w, "|") end
print()

-- nested closures over numeric state
local function make_acc(start)
    local total = start
    return function(v) total = total + v return total end
end
local acc = make_acc(10)
acc(5)
print(acc(1))

-- numeric recursion + memo table
local memo = {}
local function fib(n)
    if n < 2 then return n end
    if memo[n] then return memo[n] end
    local r = fib(n - 1) + fib(n - 2)
    memo[n] = r
    return r
end
print(fib(30))
