-- A script that only exists at run time: no build step knows about it.
print("hello from", name)

-- Hooks: the host looks these globals up by name.
function on_load()
	print("hello: loaded")
end

function on_tick(dt)
	ticks = (ticks or 0) + 1
	if ticks <= 2 then
		print("hello: tick", ticks, dt)
	end
end
