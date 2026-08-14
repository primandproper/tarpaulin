package cleanup

func Torn() string { return "torn" }

//tarp:want untested=file,package,any
func Never() string { return "never" }
