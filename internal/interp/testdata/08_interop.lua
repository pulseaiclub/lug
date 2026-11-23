-- host functions bound with lug.Bind
print(host_add(2, 3))

-- Go struct exposed with lug.Expose (userdata)
print(point.X, point.Y)
point.X = 10
print(point.X)

-- Go constructor returning a pointer
local p = make_point(3, 4)
print(p.X + p.Y)

-- userdata in tables and multiple returns
local t = {pt = p}
print(t.pt.X, t.pt.Y)
