package errorsdispatch

import (
	"errors"
	"testing"
)

func TestFindKey(t *testing.T) {
	// This assertion passes only because (*NotFoundError).Is returns true: the
	// truth of the assertion is the method's return value. The identifier `Is`
	// in the source resolves to errors.Is, so the method is still reported.
	if !errors.Is(FindKey("k"), ErrSentinel) {
		t.Fatal("want sentinel")
	}
}

func TestWrap(t *testing.T) {
	// Reachable only through (*wrappedError).Unwrap, reported the same way.
	if !errors.Is(Wrap(ErrSentinel), ErrSentinel) {
		t.Fatal("want sentinel")
	}

	// errors.As walks the same chain, and credits neither Unwrap nor the type
	// it eventually finds.
	var target *NotFoundError
	if !errors.As(Wrap(FindKey("k")), &target) {
		t.Fatal("want *NotFoundError")
	}
}
