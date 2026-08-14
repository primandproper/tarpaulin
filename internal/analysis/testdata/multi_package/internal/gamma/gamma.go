package gamma

func Tested() string { return "gamma" }

//tarp:want untested=file,package,any
func Never() string { return "never" }
