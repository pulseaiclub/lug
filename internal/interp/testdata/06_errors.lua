print(pcall(function() error("boom") end))
local ok, e = pcall(function() error({code = 42}) end)
print(ok, type(e), e.code)
local ok2, e2 = pcall(function() error("msg", 0) end)
print(ok2, e2)
print(pcall(function() return 1, 2 end))

-- nested pcall
print(pcall(function()
    local _, inner = pcall(function() error("inner") end)
    error("outer saw: " .. tostring(inner))
end))

-- runtime errors
print(pcall(function() return nil + 1 end))
print(pcall(function() local t = nil; return t.x end))
print(pcall(function() return ("x")() end))
print(pcall(function() local t = {}; return t:nosuch() end))

-- error objects are preserved through pcall
local ok3, e3 = pcall(function()
    error(setmetatable({}, {__tostring = function() return "OBJ" end}))
end)
print(ok3, tostring(e3))

-- assert
print(pcall(assert, false, "custom msg"))
print(pcall(function() assert(1 == 2) end))
print(pcall(assert, 42))

-- xpcall with handler
print(xpcall(function() error("handled") end, function(m) return "H:" .. m end))
print(xpcall(function() return "fine", 2 end, print))

-- select edge cases
print(select("#", nil, nil))
print(select(-1, "a", "b", "c"))

-- error in nested function still unwinds to pcall
local function level3() error("deep") end
local function level2() level3() end
local function level1() level2() end
print(pcall(level1))
