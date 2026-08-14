package chainedcalls

type step struct{}

func A() step { return step{} }

func (step) B() step { return step{} }

func (step) C() string { return "c" }

//tarp:want untested=file,package,any
func (step) D() string { return "d" }
