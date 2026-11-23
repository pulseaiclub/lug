-- Deliberately broken: it must not take the host down.
function on_tick(dt)
	local t = nil
	return t.field + dt
end
