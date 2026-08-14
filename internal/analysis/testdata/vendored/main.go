package vendored

import "example.com/dep"

func Local() string { return dep.Untested() }

//tarp:want untested=file,package,any
func Never() string { return "never" }
