local co = coroutine.create(function(a, b)
    print("start", a, b)
    local x = coroutine.yield(a + b)
    print("resumed with", x)
    local y, z = coroutine.yield(x * 2)
    print("resumed with", y, z)
    return "done"
end)

print(coroutine.resume(co, 1, 2))
print(coroutine.status(co))
print(coroutine.resume(co, 10))
print(coroutine.resume(co, 7, 8))
print(coroutine.status(co))
print(coroutine.resume(co))

-- wrap
local gen = coroutine.wrap(function()
    for i = 1, 3 do coroutine.yield(i) end
end)
print(gen(), gen(), gen())

-- producer/consumer
local function producer()
    return coroutine.wrap(function()
        for i = 1, 4 do coroutine.yield(i * i) end
    end)
end
local p = producer()
local sum = 0
for v in p do sum = sum + v end
print("sum", sum)

-- coroutine error handling
local bad = coroutine.create(function() error("inside") end)
print(coroutine.resume(bad))

-- isyieldable
print(coroutine.isyieldable())
local co2 = coroutine.create(function()
    return coroutine.isyieldable()
end)
print(coroutine.resume(co2))
