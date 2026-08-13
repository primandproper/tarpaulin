package subtests

func InSubtest() string { return "subtest" }

//tarp:want untested=file,package,any
func Never() string { return "never" }
