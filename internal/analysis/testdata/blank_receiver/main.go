package blankreceiver

type Thing struct{}

func (_ Thing) Blank() string { return "blank" }

//tarp:want untested=file,package,any
func (_ Thing) BlankNever() string { return "never" }
