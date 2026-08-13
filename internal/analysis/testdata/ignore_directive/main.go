package ignoredirective

//tarp:ignore -- talks to a live payment processor; covered by the e2e suite
//tarp:want excluded
func Ignored() string { return "ignored" }

// A bare directive with no reason does not exempt anything: the escape hatch
// costs one sentence, and a reasonless one is indistinguishable from giving up
// (PRD 9.2, decided: reason required).
//
//tarp:ignore
//tarp:want untested=file,package,any
func Reasonless() string { return "reasonless" }

func Fine() string { return "fine" }
