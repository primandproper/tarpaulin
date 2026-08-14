package simple

func A() string { return "A" }

// B is executed by wrapper's test, which is why statement coverage reads 100%.
//tarp:want untested=file,package,any
func B() string { return "B" }

func C() string { return "C" }

func wrapper() {
	A()
	B()
	C()
}
