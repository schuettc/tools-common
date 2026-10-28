// Package pkga holds wiregen's test types from a second package: a type
// reached across a package boundary, an enum, and a Clash that shares its
// name with pkgb.Clash.
package pkga

// Region is reached from wiregen's test roots across a package boundary.
type Region struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Kind is an enum: a named string type with Values().
type Kind string

// Values lists the kinds.
func (Kind) Values() []string { return []string{"open", "decided", "reopen"} }

// Clash has the same name as pkgb.Clash.
type Clash struct {
	A string `json:"a"`
}
