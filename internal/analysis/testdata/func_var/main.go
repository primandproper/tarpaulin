package funcvar

// Package-level func-valued vars are not function declarations (PRD 9.1,
// decided: no), so Assigned is never counted, tested or reported.
var Assigned = func() string { return "assigned" }

func Declared() string { return "declared" }

//tarp:want untested=file,package,any
func Never() string { return "never" }
