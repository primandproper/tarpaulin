package embeddedpromotion

type Base struct{}

func (Base) Shared() string { return "shared" }

//tarp:want untested=file,package,any
func (Base) Hidden() string { return "hidden" }

type Outer struct{ Base }
