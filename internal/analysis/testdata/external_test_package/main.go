package externaltestpackage

func Exported() string { return "exported" }

//tarp:want untested=file,package,any
func AlsoExported() string { return "also" }
