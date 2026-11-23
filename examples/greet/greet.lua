-- greet.lua -- an example plugin-style script.
--
-- The host passes the script's arguments after `...`, exposes a Go value as
-- `player` and binds a Go function as `roll`. Everything else is plain Lua.

local args = { ... }
local who = args[1] or "world"

-- Exposed Go structs keep their Go field names, and pointer-receiver methods
-- are callable with `:`.
print("hello, " .. who)
local before = player.HP
player:Damage(25)
print("HP:", before, "->", player.HP)

-- A bound Go function: ordinary Go signature, no Lua types at the boundary.
print("roll(6):", roll(6))

-- Errors propagate to Go as an error value; there is no pcall needed here.
if who == "boom" then
	error("boom was requested")
end

-- A function the host keeps calling after the chunk finished.
function greet(name)
	print("greet:", name)
	return #name
end

-- Return several values to Go.
return who, player.HP, #who
