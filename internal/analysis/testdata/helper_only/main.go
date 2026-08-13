package helperonly

// ViaHelper is only ever reached through a test helper. At `file` and `package`
// strictness a reference must sit lexically inside a TestXxx body, so this is
// reported; `any` accepts helpers and credits it.
//
//tarp:want untested=file,package
func ViaHelper() string { return "helper" }

func Direct() string { return "direct" }
