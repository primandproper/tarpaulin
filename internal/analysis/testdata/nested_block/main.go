package nestedblock

func Inner() string { return "inner" }

//tarp:want untested=file,package,any
func Never() string { return "never" }
