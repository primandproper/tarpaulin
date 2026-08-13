package elsebranch

func Taken() string { return "taken" }

func Untaken() string { return "untaken" }

//tarp:want untested=file,package,any
func Never() string { return "never" }
