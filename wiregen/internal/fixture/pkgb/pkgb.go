// Package pkgb holds wiregen's test types: a Clash that shares its name with
// pkga.Clash, and a Record that shadows TypeScript's utility type.
package pkgb

// Clash has the same name as pkga.Clash.
type Clash struct {
	B string `json:"b"`
}

// Record is declared under TypeScript's own utility type name.
type Record struct {
	Counts map[string]int `json:"counts"`
}
