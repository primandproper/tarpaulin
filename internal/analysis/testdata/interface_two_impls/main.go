package interfacetwoimpls

// Two types implement Doer, so a dispatch through the interface is credited to
// neither of them (PRD 3.6): the tool cannot know which one ran.
type Doer interface {
	Do() string
}

type First struct{}

//tarp:want untested=file,package,any
func (First) Do() string { return "first" }

type Second struct{}

//tarp:want untested=file,package,any
func (Second) Do() string { return "second" }

func Dispatch(d Doer) string { return d.Do() }
