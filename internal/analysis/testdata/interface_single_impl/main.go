package interfacesingleimpl

// Impl is the only type in this package that implements Doer, so a test that
// dispatches through the interface gets credited to Impl.Do.
type Doer interface {
	Do() string
}

type Impl struct{}

func (Impl) Do() string { return "impl" }

func NewDoer() Doer { return Impl{} }
