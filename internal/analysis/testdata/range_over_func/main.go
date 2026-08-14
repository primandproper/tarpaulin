package rangeoverfunc

// Seq is invoked by `for x := range Seq` without any CallExpr appearing in the
// test source: asking "is this a call?" cannot see it, asking "what does this
// identifier resolve to?" can.
func Seq(yield func(int) bool) {
	for i := range 3 {
		if !yield(i) {
			return
		}
	}
}

//tarp:want untested=file,package,any
func Never() string { return "never" }
