package internalandexternal

// Go forbids package internalandexternal_test from referencing unexported, so
// the only way to test it is a second, internal test file. The test slot for
// thing.go is therefore the pair {thing_test.go, thing_internal_test.go} — both
// are accepted, which is what makes the file-level rule fixable rather than
// merely strict (PRD 3.3).
func Exported() string { return unexported() }

func unexported() string { return "unexported" }

//tarp:want untested=file,package,any
func alsoUnexported() string { return "also" }
