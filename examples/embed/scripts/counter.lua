local counted = 0
function on_tick(dt)
	counted = counted + dt
end
function on_unload()
	print("counter: total " .. counted)
end
