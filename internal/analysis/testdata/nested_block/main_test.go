package nestedblock

import "testing"

func TestInner(t *testing.T) {
	{
		{
			Inner()
		}
	}
}
