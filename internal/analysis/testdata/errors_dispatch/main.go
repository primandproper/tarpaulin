// Package errorsdispatch pins what happens to the errors trio — Is, As, and
// Unwrap — when a test exercises them only through the errors package's own
// walk. Nothing here is credited: `errors.Is(err, ErrSentinel)` names
// errors.Is, and the walk from that value to the method declared below is a
// runtime property of a value, not a reference the type checker can hand back.
//
// Crediting it would take connecting the argument to the type declaring the
// method, which is dataflow the analyzer deliberately does not do. Crediting it
// by package instead — any errors.Is in a test absolving every Is/As/Unwrap
// beside it — is the over-crediting `go test -cover` already does, and is the
// thing this tool exists not to do.
package errorsdispatch

import "errors"

// ErrSentinel is what the tests below assert against.
var ErrSentinel = errors.New("sentinel")

// NotFoundError answers Is for the sentinel, so a test asserting
// errors.Is(err, ErrSentinel) is asserting exactly this method's return value —
// and still cannot name it.
type NotFoundError struct {
	Key string
}

//tarp:want untested=file,package,any
func (e *NotFoundError) Error() string { return "not found: " + e.Key }

//tarp:want untested=file,package,any
func (e *NotFoundError) Is(target error) bool { return target == ErrSentinel }

// wrappedError carries an inner error the errors walk reaches through Unwrap.
type wrappedError struct {
	inner error
}

//tarp:want untested=file,package,any
func (e *wrappedError) Error() string { return "wrapped: " + e.inner.Error() }

//tarp:want untested=file,package,any
func (e *wrappedError) Unwrap() error { return e.inner }

// Wrap is named by the test, so it is credited; the Unwrap it depends on is not.
func Wrap(err error) error { return &wrappedError{inner: err} }

// FindKey is named by the test for the same reason.
func FindKey(key string) error { return &NotFoundError{Key: key} }
