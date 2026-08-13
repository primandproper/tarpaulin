package crossfile

// Cross is declared in a.go but only referenced from b_test.go. a.go's test
// slot is {a_test.go, a_internal_test.go}, neither of which exists, so this
// fails at `file` and passes once the dial is loosened to `package`.
//
//tarp:want untested=file
func Cross() string { return "cross" }
