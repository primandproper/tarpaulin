package methodvalue

type Thing struct{}

func (Thing) Bound() string { return "bound" }

//tarp:want untested=file,package,any
func (Thing) Unbound() string { return "unbound" }
