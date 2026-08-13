package externalonlyunexported

func Exported() string { return unexported() }

// The whole of §3.3 in one declaration. This package's only test file is the
// external thing_test.go, in package externalonlyunexported_test, and Go forbids
// that package from referencing an unexported identifier: there is no legal way
// to write a test that names unexported from where the tests currently live.
//
// So it is reported at every position on the dial, including any. That is not
// strictness — no setting can rescue a test that would not compile. The fix is a
// second test file in package externalonlyunexported, which is exactly what the
// internal_and_external fixture shows, and why the file-level slot is the pair
// {thing_test.go, thing_internal_test.go} rather than thing_test.go alone.
//
//tarp:want untested=file,package,any
func unexported() string { return "unexported" }
