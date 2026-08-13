package builtins

func Clamp(lo, hi, v int) int { return min(hi, max(lo, v)) }
