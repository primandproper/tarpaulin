package main

//tarp:want excluded
func init() {
	_ = Real()
}

//tarp:want excluded
func main() {
	_ = Real()
}

func Real() string { return "real" }

//tarp:want untested=file,package,any
func Never() string { return "never" }
