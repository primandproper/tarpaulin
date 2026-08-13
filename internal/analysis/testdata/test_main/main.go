package testmain

// Harnessed is referenced only from TestMain, which is process setup rather
// than a test: it asserts nothing about Harnessed's behavior. It counts only at
// `any`, where every reference in a test file counts.
//
//tarp:want untested=file,package
func Harnessed() string { return "harnessed" }

func Tested() string { return "tested" }
