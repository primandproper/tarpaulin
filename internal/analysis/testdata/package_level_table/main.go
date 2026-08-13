package packageleveltable

func Tabled() string { return "tabled" }

// Orphan is referenced by a package-level var in the test file that no TestXxx
// body ever touches. The one-hop rule (PRD 3.5) follows references *from* a
// test body into a var initializer — it does not sweep every initializer in the
// file, so Orphan stays untested until the strictness dial reaches `any`.
//
//tarp:want untested=file,package
func Orphan() string { return "orphan" }

//tarp:want untested=file,package,any
func Never() string { return "never" }
