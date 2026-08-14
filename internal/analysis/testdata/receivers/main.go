package receivers

type Val struct{}

func (Val) Ping() string { return "val" }

type Ptr struct{}

func (*Ptr) Ping() string { return "ptr" }

// Other.Ping shares a name with the two methods above and must not inherit
// their credit: functions are identified by declaration position, not by name.
type Other struct{}

//tarp:want untested=file,package,any
func (Other) Ping() string { return "other" }
