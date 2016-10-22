package luar

import (
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"testing"
)

// Calling Go functions from Lua.
// Returning multiple values is straightforward.
// All Go number types map to Lua numbers, which are (usually) doubles.
//
// Arbitrary Go functions can be registered to be callable from Lua. Here the
// functions are put into the global table.
func TestGoFunCall(t *testing.T) {
	id := func(x float32, a string) (float32, string) {
		return x, a
	}

	sum := func(args []float64) float64 {
		res := 0.0
		for _, val := range args {
			res += val
		}
		return res
	}

	sumv := func(args ...float64) float64 {
		return sum(args)
	}

	// [10,20] -> {'0':100, '1':400}
	squares := func(args []int) (res map[string]int) {
		res = make(map[string]int)
		for i, val := range args {
			res[strconv.Itoa(i)] = val * val
		}
		return
	}

	IsNilInterface := func(v interface{}) bool {
		return v == nil
	}

	IsNilPointer := func(v *person) bool {
		return v == nil
	}

	tdt := []struct{ desc, code string }{
		{"go function call", `x, a = id(42, 'foo')
assert(x == 42 and a == 'foo')`},
		{"auto-convert table to slice", `res = sum{1, 10, 100}
assert(res == 111)`},
		{"variadic call", `res = sumv(1, 10, 100)
assert(res == 111)`},

		// A map is returned as a map-proxy, which we may explicitly convert to a
		// table.
		{"init proxy", `proxy = squares{10, 20}
assert(proxy['0'] == 100)
assert(proxy['1'] == 400)`},
		{"copy proxy to table", `proxy = squares{10, 20}
t = luar.map2table(proxy)
assert(type(t)=='table')
assert(t['0'] == 100)
assert(t['1'] == 400)`},
		{"change proxy, not table", `proxy = squares{10, 20}
t = luar.map2table(proxy)
proxy['0'] = 0
assert(t['0'] == 100)`},

		{"pass nil to Go functions", `assert(IsNilInterface(nil))
assert(IsNilPointer(nil))`},
	}

	for _, v := range tdt {
		L := Init()
		defer L.Close()
		Register(L, "", Map{
			"id":             id,
			"sum":            sum,
			"sumv":           sumv,
			"squares":        squares,
			"IsNilInterface": IsNilInterface,
			"IsNilPointer":   IsNilPointer,
		})
		err := L.DoString(v.code)
		if err != nil {
			t.Error(v.desc+":", err)
		}
	}
}

func TestNamespace(t *testing.T) {
	keys := func(m map[string]interface{}) (res []string) {
		res = make([]string, 0)
		for k := range m {
			res = append(res, k)
		}
		return
	}

	values := func(m map[string]interface{}) (res []interface{}) {
		res = make([]interface{}, 0)
		for _, v := range m {
			res = append(res, v)
		}
		return
	}

	const code = `
-- Passing a 'hash-like' Lua table converts to a Go map.
local T = {one=1, two=2}
local k = gons.keys(T)

-- Can't depend on deterministic ordering in returned slice proxy.
assert( (k[1]=='one' and k[2]=='two') or (k[2]=='one' and k[1]=='two') )

local v = gons.values(T)
assert(v[1]==1 or v[2]==1)
v = luar.slice2table(v)
assert( (v[1]==1 and v[2]==2) or (v[2]==1 and v[1]==2) )`

	L := Init()
	defer L.Close()

	Register(L, "gons", Map{
		"keys":   keys,
		"values": values,
	})

	err := L.DoString(code)
	if err != nil {
		t.Error(err)
	}
}

type person struct {
	Name string
	Age  int
}

type hasName interface {
	GetName() string
}

func (p *person) GetName() string {
	return p.Name
}

func newPerson(name string, age int) *person {
	return &person{name, age}
}

func newName(t *person) hasName {
	return t
}

func getName(o hasName) string {
	return o.GetName()
}

func TestStructAccess(t *testing.T) {
	const code = `
-- 't' is a struct proxy.
-- We can always directly get and set public fields.
local t = NewPerson("Alice", 16)
assert(t.Name == 'Alice')
assert(t.Age == 16)
t.Name = 'Caterpillar'

-- Note a pitfall: we don't use colon notation here.
assert(t.GetName() == 'Caterpillar')

-- Interfaces.
t = NewPerson("Alice", 16)
it = NewName(t)
assert(it.GetName()=='Alice')
assert(GetName(it)=='Alice')
assert(GetName(t)=='Alice')
assert(luar.type(t).String() == "*luar.person")
assert(luar.type(it).String() == "*luar.person")
`

	L := Init()
	defer L.Close()

	Register(L, "", Map{
		"NewPerson": newPerson,
		"NewName":   newName,
		"GetName":   getName,
	})

	err := L.DoString(code)
	if err != nil {
		t.Error(err)
	}
}

func TestInterfaceAccess(t *testing.T) {
	const code = `
-- Calling methods on an interface.
local f, err = OsOpen("luar_test.go")
local buff = byteBuffer(100)
assert(#buff == 100)
local k, err = f.Read(buff)
assert(k == 100)
local s = bytesToString(buff)
assert(s:match '^package luar')
f.Close()`

	// There are some basic constructs which need help from the Go side...
	// Fortunately it's very easy to import them!
	byteBuffer := func(sz int) []byte {
		return make([]byte, sz)
	}
	bytesToString := func(bb []byte) string {
		return string(bb)
	}

	L := Init()
	defer L.Close()

	Register(L, "", Map{
		"OsOpen":        os.Open,
		"byteBuffer":    byteBuffer,
		"bytesToString": bytesToString,
	})

	err := L.DoString(code)
	if err != nil {
		t.Error(err)
	}
}

func TestLuaCallSlice(t *testing.T) {
	L := Init()
	defer L.Close()

	const code = `
Libs = {}
function Libs.fun(s,i,t,m)
	assert(s == 'hello')
	assert(i == 42)
    --// slices and maps passed as proxies
	assert(type(t) == 'userdata' and t[1] == 42)
	assert(type(m) == 'userdata' and m.name == 'Joe')
	return 'ok'
end`

	err := L.DoString(code)
	if err != nil {
		t.Error(err)
	}

	fun := NewLuaObjectFromName(L, "Libs.fun")
	got, _ := fun.Call("hello", 42, []int{42, 66, 104}, map[string]string{
		"name": "Joe",
	})
	if got != "ok" {
		t.Error("did not get correct slice of slices!")
	}
}

func TestLuaCallfSlice(t *testing.T) {
	L := Init()
	defer L.Close()

	const code = `
function return_slices()
    return {{'one'}, luar.null, {'three'}}
end`

	err := L.DoString(code)
	if err != nil {
		t.Error(err)
	}

	fun := NewLuaObjectFromName(L, "return_slices")
	results, _ := fun.Callf(Types([][]string{}))
	sstrs := results[0].([][]string)
	if !(sstrs[0][0] == "one" && sstrs[1] == nil && sstrs[2][0] == "three") {
		t.Error("did not get correct slice of slices!")
	}
}

// See if Go values are properly anchored.
func TestAnchoring(t *testing.T) {
	L := Init()
	defer L.Close()

	const code = `local s = luar.slice(2)
s[1] = 10
s[2] = 20
gc()
assert(#s == 2 and s[1]==10 and s[2]==20)
s = nil`

	Register(L, "", Map{
		"gc": runtime.GC,
	})

	err := L.DoString(code)
	if err != nil {
		t.Error(err)
	}
}

type A int

func NewA(i int) A {
	return A(i)
}

func (a A) String() string {
	return strconv.Itoa(int(a))
}

type B int

func TestTypeDiscipline(t *testing.T) {
	tdt := []struct{ desc, code string }{
		{"call methods on objects 'derived' from primitive types", `assert(a.String() == '5')`},
		{"get underlying primitive value", `assert(luar.raw(a) == 5)`},
		{"arith ops on derived types", `assert(new_a(8) == new_a(8))
assert(new_a(5) ~= new_a(6))
-- TODO: Arith ops on userdata does not work, why?
-- assert(new_a(5) < new_a(8))
-- assert(new_a(8) > new_a(5))
-- assert(((new_a(8) * new_a(5)) / new_a(4)) % new_a(7) == new_a(3))`},
	}

	L := Init()
	defer L.Close()

	a := A(5)
	b := B(9)

	Register(L, "", Map{
		"a":     a,
		"b":     b,
		"new_a": NewA,
	})

	for _, v := range tdt {
		err := L.DoString(v.code)
		if err != nil {
			t.Error(v.desc, err)
		}
	}

	L.GetGlobal("a")
	aType := reflect.TypeOf(a)
	al := LuaToGo(L, aType, -1)
	alType := reflect.TypeOf(al)

	if alType != aType {
		t.Error("types were not converted properly")
	}

	// Binary op with different type must fail.
	const fail = `assert(b != new_a(9))`
	err := L.DoString(fail)
	if err == nil {
		t.Error(err)
	}
}

// Map non-existent entry should be nil.
func TestTypeMap(t *testing.T) {
	L := Init()
	defer L.Close()

	m := map[string]string{"test": "art"}

	Register(L, "", Map{
		"m": m,
	})

	const code = `assert(m.test == 'art')
assert(m.Test == nil)`

	// Accessing map with wrong key type must fail.
	const code2 = `_=m[5]`

	err := L.DoString(code)
	if err != nil {
		t.Error(err)
	}
}

// 'nil' in Go slices and maps is represented by luar.null.
func TestTypeConversion(t *testing.T) {
	L := Init()
	defer L.Close()

	const code = `
tab = luar.slice2table(sl)
assert(#tab == 4)
assert(tab[1] == luar.null)
assert(tab[3] == luar.null)

tab2 = luar.map2table(mn)
assert(tab2.bee == luar.null and tab2.dee == luar.null)
`

	sl := [][]int{
		nil,
		{1, 2},
		nil,
		{10, 20},
	}

	mn := map[string][]int{
		"aay": {1, 2},
		"bee": nil,
		"cee": {10, 20},
		"dee": nil,
	}

	Register(L, "", Map{
		"sl": sl,
		"mn": mn,
	})

	err := L.DoString(code)
	if err != nil {
		t.Error(err)
	}
}

func BenchmarkLuaToGoSliceInt(b *testing.B) {
	L := Init()
	defer L.Close()

	var output []interface{}
	L.DoString(`t={}; for i = 1,100 do t[i]=i; end`)
	L.GetGlobal("t")
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = LuaToGo(L, reflect.TypeOf(output), -1)
	}
}

func BenchmarkLuaToGoSliceMap(b *testing.B) {
	L := Init()
	defer L.Close()

	var output []interface{}
	L.DoString(`t={}; s={17}; for i = 1,100 do t[i]=s; end`)
	L.GetGlobal("t")
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = LuaToGo(L, reflect.TypeOf(output), -1)
	}
}

func BenchmarkLuaToGoSliceMapUnique(b *testing.B) {
	L := Init()
	defer L.Close()

	var output []interface{}
	L.DoString(`t={}`)
	for i := 0; i < 100; i++ {
		L.DoString(fmt.Sprintf(`s%[1]d={17}; t[%[1]d]=s%[1]d`, i))
	}
	L.GetGlobal("t")
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = LuaToGo(L, reflect.TypeOf(output), -1)
	}
}

func BenchmarkLuaToGoMapInt(b *testing.B) {
	L := Init()
	defer L.Close()

	var output map[string]interface{}
	L.DoString(`t={}; for i = 1,100 do t[tostring(i)]=i; end`)
	L.GetGlobal("t")
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = LuaToGo(L, reflect.TypeOf(output), -1)
	}
}

func BenchmarkLuaToGoMapSlice(b *testing.B) {
	L := Init()
	defer L.Close()

	var output map[string]interface{}
	L.DoString(`t={}; s={17}; for i = 1,100 do t[tostring(i)]=s; end`)
	L.GetGlobal("t")
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = LuaToGo(L, reflect.TypeOf(output), -1)
	}
}

func BenchmarkLuaToGoMapSliceUnique(b *testing.B) {
	L := Init()
	defer L.Close()

	var output map[string]interface{}
	L.DoString(`t={}`)
	for i := 0; i < 100; i++ {
		L.DoString(fmt.Sprintf(`s%[1]d={17}; t["%[1]d"]=s%[1]d`, i))
	}
	L.GetGlobal("t")
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = LuaToGo(L, reflect.TypeOf(output), -1)
	}
}

func BenchmarkGoToLuaSliceInt(b *testing.B) {
	L := Init()
	defer L.Close()

	input := make([]int, 100)
	for i := 0; i < 100; i++ {
		input[i] = i
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		GoToLua(L, nil, reflect.ValueOf(input), true)
		L.SetTop(0)
	}
}

func BenchmarkGoToLuaSliceSlice(b *testing.B) {
	L := Init()
	defer L.Close()

	sub := []int{17}
	input := make([][]int, 100)
	for i := 0; i < 100; i++ {
		input[i] = sub
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		GoToLua(L, nil, reflect.ValueOf(input), true)
		L.SetTop(0)
	}
}

func BenchmarkGoToLuaSliceSliceUnique(b *testing.B) {
	L := Init()
	defer L.Close()

	input := make([][]int, 100)
	for i := 0; i < 100; i++ {
		input[i] = []int{17}
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		GoToLua(L, nil, reflect.ValueOf(input), true)
		L.SetTop(0)
	}
}

func BenchmarkGoToLuaMapInt(b *testing.B) {
	L := Init()
	defer L.Close()

	input := map[int]int{}
	for i := 0; i < 100; i++ {
		input[i] = i
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		GoToLua(L, nil, reflect.ValueOf(input), true)
		L.SetTop(0)
	}
}

func BenchmarkGoToLuaMapSlice(b *testing.B) {
	L := Init()
	defer L.Close()

	sub := []int{17}
	input := map[int][]int{}
	for i := 0; i < 100; i++ {
		input[i] = sub
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		GoToLua(L, nil, reflect.ValueOf(input), true)
		L.SetTop(0)
	}
}

func BenchmarkGoToLuaMapSliceUnique(b *testing.B) {
	L := Init()
	defer L.Close()

	input := map[int][]int{}
	for i := 0; i < 100; i++ {
		input[i] = []int{17}
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		GoToLua(L, nil, reflect.ValueOf(input), true)
		L.SetTop(0)
	}
}
