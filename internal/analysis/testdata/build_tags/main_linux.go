//go:build linux

package buildtags

func OnLinux() string { return "linux" }

//tarp:want untested=file,package,any
func NeverOnLinux() string { return "never" }
