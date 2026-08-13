package genericmethods

type Box[T any] struct{ value T }

func NewBox[T any](v T) Box[T] { return Box[T]{value: v} }

func (b Box[T]) Get() T { return b.value }

//tarp:want untested=file,package,any
func (b Box[T]) Unused() T { return b.value }
