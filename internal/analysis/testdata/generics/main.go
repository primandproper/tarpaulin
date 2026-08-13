package generics

func Map[T, U any](in []T, fn func(T) U) []U {
	out := make([]U, 0, len(in))
	for _, v := range in {
		out = append(out, fn(v))
	}

	return out
}

func Identity[T any](v T) T { return v }

//tarp:want untested=file,package,any
func Never[T any](v T) T { return v }
