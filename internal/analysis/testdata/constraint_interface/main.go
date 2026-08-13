package constraintinterface

// Labeler's method is not a function declaration: interface methods have no
// body, so they must never appear in the declared set (and therefore can never
// be reported as untested).
type Labeler interface {
	Label() string
}

type Thing struct{}

func (Thing) Label() string { return "thing" }

func Join[T Labeler](xs []T) string {
	out := ""
	for _, x := range xs {
		out += x.Label()
	}

	return out
}
