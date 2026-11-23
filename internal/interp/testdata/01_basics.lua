-- arithmetic and precedence
print(1 + 2 * 3, (1 + 2) * 3, 10 / 4, 7 % 3, -7 % 3, 2 ^ 10)
print(10 // 3, 7 // 2, -2 ^ 2)
print(1 .. 2)
print("a" .. "b" .. 1 + 2)

-- locals, multiple assignment
local a, b = 1, 2
a, b = b, a
print(a, b)

-- strings
local s = "hello"
print(#s, s:upper(), string.sub(s, 2, 3), s .. "!")
print(string.format("%d-%s-%.2f-%q", 42, "x", 3.14159, 'a"b\n'))
print(("abc"):rep(3, "-"))
print(string.byte("A"), string.char(72, 105))

-- if / elseif / else
local function grade(n)
    if n >= 90 then return "A"
    elseif n >= 80 then return "B"
    else return "C" end
end
print(grade(95), grade(85), grade(50))

-- while
local i = 0
while i < 5 do i = i + 1 end
print("while", i)

-- repeat
local j = 10
repeat j = j - 3 until j <= 0
print("repeat", j)

-- numeric for
local acc = {}
for k = 1, 10, 2 do acc[#acc+1] = k end
print(table.concat(acc, ","))

for k = 10, 1, -3 do io.write(k, " ") end
io.write("\n")

-- break
for k = 1, 100 do if k > 3 then break end io.write(k) end
io.write("\n")

-- goto continue
local evens = {}
for k = 1, 10 do
    if k % 2 ~= 0 then goto continue end
    evens[#evens+1] = k
    ::continue::
end
print("evens", table.concat(evens, " "))

-- varargs and select
local function varargs(...)
    local n = select("#", ...)
    local first = select(1, ...)
    return n, first, ...
end
print(varargs("a", "b", "c"))

-- multiple returns
local function multi() return 1, 2, 3 end
local x, y, z, w = multi()
print(x, y, z, w)
print((multi()))
local t = {multi()}
print(#t, t[1], t[3])
