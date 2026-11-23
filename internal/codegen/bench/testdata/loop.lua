-- A numeric for loop with arithmetic in the body.
local function sumloop(n)
    local sum = 0
    for i = 1, n do sum = sum + i % 7 end
    return sum
end
return sumloop(100000)
