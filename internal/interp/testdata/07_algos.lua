-- quicksort in place
local function qsort(t, lo, hi)
    lo = lo or 1
    hi = hi or #t
    if lo < hi then
        local p = t[hi]
        local i = lo - 1
        for j = lo, hi - 1 do
            if t[j] <= p then
                i = i + 1
                t[i], t[j] = t[j], t[i]
            end
        end
        t[i+1], t[hi] = t[hi], t[i+1]
        qsort(t, lo, i)
        qsort(t, i + 2, hi)
    end
    return t
end
print(table.concat(qsort({5, 2, 9, 1, 7, 3}), ","))

-- table.sort with and without comparator
local words = {"banana", "apple", "cherry"}
table.sort(words)
print(table.concat(words, " "))
table.sort(words, function(a, b) return a > b end)
print(table.concat(words, " "))

-- sieve of Eratosthenes
local function primes(n)
    local sieve = {}
    local out = {}
    for i = 2, n do
        if not sieve[i] then
            out[#out+1] = i
            for j = i * i, n, i do sieve[j] = true end
        end
    end
    return out
end
print(table.concat(primes(30), " "))

-- insert / remove
local stack = {}
table.insert(stack, "a")
table.insert(stack, 1, "front")
table.insert(stack, 3, "back")
print(table.concat(stack, ","))
print(table.remove(stack), table.remove(stack, 1), #stack)

-- unpack / pack
local t = {10, 20, 30}
print(table.unpack(t))
local packed = table.pack(1, nil, 3)
print(packed.n, packed[1], packed[3])

-- matrix multiply
local function matmul(a, b)
    local c = {}
    for i = 1, #a do
        c[i] = {}
        for j = 1, #b[1] do
            local s = 0
            for k = 1, #b do s = s + a[i][k] * b[k][j] end
            c[i][j] = s
        end
    end
    return c
end
local m = matmul({{1, 2}, {3, 4}}, {{5, 6}, {7, 8}})
print(m[1][1], m[1][2], m[2][1], m[2][2])

-- generic for with pairs (deterministic insertion order) and next
local p = {one = 1, two = 2, three = 3}
local keys = {}
for k in pairs(p) do keys[#keys+1] = k end
print(table.concat(keys, ","))

-- math library
print(math.floor(3.7), math.ceil(3.2), math.abs(-5), math.max(1, 9, 3), math.min(4, 2, 8))
print(math.sqrt(16), math.huge > 1e308, math.pi)
print(math.fmod(7, 3), math.modf(3.75))
print(math.maxinteger >= 9e18, math.mininteger <= -9e18)

-- string coercion in arithmetic and concat
print("10" + 5, "3" * "4", 10 .. 20)
