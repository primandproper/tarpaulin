package interfacesingleimpl

// Impl is the only type in this package that implements Doer, so a test that
// dispatches through the interface gets credited to Impl.Do (PRD 3.6, rung 1).
type Doer interface {
	Do() string
}

type Impl struct{}

func (Impl) Do() string { return "impl" }

func NewDoer() Doer { return Impl{} }
