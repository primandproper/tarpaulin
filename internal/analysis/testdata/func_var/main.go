package funcvar

// Package-level func-valued vars are deliberately not function declarations,
// so Assigned is never counted, tested or reported.
var Assigned = func() string { return "assigned" }

func Declared() string { return "declared" }

//tarp:want untested=file,package,any
func Never() string { return "never" }
