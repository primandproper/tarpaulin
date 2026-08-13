package methodexpression

type Thing struct{}

func (Thing) Expressed() string { return "expressed" }

//tarp:want untested=file,package,any
func (Thing) Unexpressed() string { return "unexpressed" }
