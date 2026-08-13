package alpha

func Tested() string { return "alpha" }

//tarp:want untested=file,package,any
func Never() string { return "never" }
