-- Recursive fib: a direct-call, all-numeric function.
local function fib(k)
    if k < 2 then return k end
    return fib(k - 1) + fib(k - 2)
end
return fib(24)
