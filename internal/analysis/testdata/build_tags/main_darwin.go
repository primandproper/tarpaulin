//go:build darwin

package buildtags

func OnDarwin() string { return "darwin" }

//tarp:want untested=file,package,any
func NeverOnDarwin() string { return "never" }
