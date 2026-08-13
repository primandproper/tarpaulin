package nestedselector

// The old README documented this as a "Known Issue": a call reached through a
// chain of field selectors was never attributed to the method it invoked.

type C struct{}

func (C) Method() string { return "method" }

//tarp:want untested=file,package,any
func (C) Unmethod() string { return "unmethod" }

type B struct{ C C }

type A struct{ B B }

func NewA() A { return A{} }
