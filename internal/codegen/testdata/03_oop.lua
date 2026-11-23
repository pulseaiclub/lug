-- class pattern with metatables and inheritance
local Animal = {}
Animal.__index = Animal

function Animal.new(name, sound)
    return setmetatable({name = name, sound = sound}, Animal)
end

function Animal:speak()
    return self.name .. " says " .. self.sound
end

local Dog = setmetatable({}, {__index = Animal})
Dog.__index = Dog

function Dog.new(name)
    return setmetatable(Animal.new(name, "woof"), Dog)
end

function Dog:fetch()
    return self.name .. " fetches"
end

local a = Animal.new("Cat", "meow")
local d = Dog.new("Rex")
print(a:speak())
print(d:speak()) -- inherited through __index chain
print(d:fetch())

-- metamethods: __eq __lt __len __concat __call __tostring
local V = {}
V.__index = V
V.__tostring = function(v) return "V(" .. v[1] .. "," .. v[2] .. ")" end
V.__eq = function(x, y) return x[1] == y[1] and x[2] == y[2] end
V.__lt = function(x, y) return x[1] < y[1] end
V.__len = function() return 99 end
V.__concat = function(x, y)
    local xs = type(x) == "table" and tostring(x) or x
    local ys = type(y) == "table" and tostring(y) or y
    return xs .. "+" .. ys
end
V.__call = function(self, arg) return "called with " .. arg end

local v1 = setmetatable({1, 2}, V)
local v2 = setmetatable({1, 2}, V)
local v3 = setmetatable({3, 4}, V)
print(v1 == v2, v1 == v3, v1 < v3, #v1)
print(tostring(v1))
print(v1 .. "x")
print(v1("hi"))

-- __index as a function
local proto = setmetatable({}, {__index = function(t, k) return k .. "!" end})
print(proto.foo, proto.bar)

-- __newindex
local ro = setmetatable({}, {
    __newindex = function(t, k, v) rawset(t, k, "RO:" .. tostring(v)) end,
})
ro.x = 1
print(ro.x)

-- protected metatable
local prot = setmetatable({}, {__metatable = "locked"})
print(getmetatable(prot))
print(pcall(setmetatable, prot, {}))
