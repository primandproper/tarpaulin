package forclauses

func Init() int { return 0 }

func Cond(i int) bool { return i < 1 }

func Post(i int) int { return i + 1 }

//tarp:want untested=file,package,any
func Never() string { return "never" }
