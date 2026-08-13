package dep

// Untested is a dependency's function: vendored code is never analyzed, so it
// must not appear in the report no matter how untested it is.
func Untested() string { return "untested" }
