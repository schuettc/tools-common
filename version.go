package tools

// Version holds the tool's own ldflags-stamped build metadata. tools-common
// does not own these vars; each family binary stamps its own via
// `-ldflags -X` and passes the values in through Config.
type Version struct {
	Number string
	Commit string
	Date   string
}

// String renders the family-wide version format:
//
//	"<Number> (<Commit>, <Date>)"
//
// Elision rules:
//   - Commit=="" && Date=="" → "<Number>"
//   - Date=="" only          → "<Number> (<Commit>)"
//   - Number=="" (unset)     → "dev"
func (v Version) String() string {
	if v.Number == "" {
		return "dev"
	}
	switch {
	case v.Commit == "" && v.Date == "":
		return v.Number
	case v.Date == "":
		return v.Number + " (" + v.Commit + ")"
	default:
		return v.Number + " (" + v.Commit + ", " + v.Date + ")"
	}
}
