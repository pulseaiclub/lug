-- this script crashes on purpose: the harness must report the Lua error
print("before")
local t = nil
print(t.field)
print("unreachable")
