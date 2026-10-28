// Package wiregen writes a page tool's wire.d.ts, the TypeScript declarations
// of the JSON its server sends, from the Go structs themselves, and checks the
// committed file against them. A tool's wire_test.go names its roots and calls
// CheckCommitted, so a renamed field fails `go test` on every machine, with or
// without node.
//
// The declarations follow encoding/json:
//
//   - a struct is an interface, walked breadth-first from the roots in the
//     order its types are first reached, across package boundaries; an
//     untagged embedded struct's fields are promoted into its parent;
//   - `x?: T` when the field has omitempty or omitzero (absent when zero);
//     `x: T | null` for a slice, map, pointer or interface without them;
//   - a time.Time is a string, and so is a []byte (base64) and any type
//     with a MarshalText method (on the value or its pointer);
//   - a json.RawMessage is unknown, and so is any other type with its own
//     MarshalJSON, since only its code knows the shape: Generate refuses
//     both unless Options.AllowUnknown is set;
//   - a named string type with a `Values() []string` method is an enum,
//     declared as a union of its values (tools-common's msgstate.State is
//     MessageState, the name every family page knows it by);
//   - a map is `Record<K, V>`, or `{ [key: K]: V }` when the file declares a
//     type named Record, which would shadow the utility type.
//
// Generate refuses two Go types with one TypeScript name, and a field it can
// only declare as unknown unless Options.AllowUnknown is set.
package wiregen

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/subaud-tools/tools-common/msgstate"
)

// Options is what differs between tools.
type Options struct {
	// Header is written verbatim at the top of the file: the tool's own
	// comment saying what generated it and how to regenerate it.
	Header string
	// Regenerate is the command named in CheckCommitted's errors, such as
	// "just wire". Empty names `go test -update`.
	Regenerate string
	// AllowUnknown lets a field whose type has no TypeScript shape (an
	// interface, a func, an anonymous struct, a json.RawMessage, a type with
	// its own MarshalJSON) be declared as unknown. Off, it is an error naming
	// the field (and the type, for a json.Marshaler).
	AllowUnknown bool
}

// Generate renders the declaration file for roots, each a value of a root
// type (PendingView{}), in the order given.
func Generate(roots []any, opts Options) (string, error) {
	g := &gen{seen: map[reflect.Type]bool{}, names: map[string]reflect.Type{}}
	for _, root := range roots {
		g.found(reflect.TypeOf(root))
	}
	// The first pass reaches every type, so the second knows whether a type
	// named Record is declared before it writes the first map.
	for i := 0; i < len(g.queue); i++ {
		g.render(g.queue[i])
	}
	if len(g.conflicts) > 0 {
		return "", errors.Join(g.conflicts...)
	}
	_, g.shadowed = g.names["Record"]
	g.unknown = nil
	var b strings.Builder
	b.WriteString(opts.Header)
	for _, t := range g.queue {
		b.WriteString("\n")
		b.WriteString(g.render(t))
	}
	if len(g.unknown) > 0 && !opts.AllowUnknown {
		return "", fmt.Errorf("wiregen: no TypeScript shape for %s (declared unknown; set AllowUnknown to allow it)", strings.Join(g.unknown, ", "))
	}
	return b.String(), nil
}

// CheckCommitted compares the file at path with Generate(roots, opts) and
// returns an error naming the first line that differs. With update it writes
// the file instead (creating its directory).
func CheckCommitted(path string, roots []any, opts Options, update bool) error {
	want, err := Generate(roots, opts)
	if err != nil {
		return err
	}
	path = filepath.Clean(path)
	fix := opts.Regenerate
	if fix == "" {
		fix = "go test -update"
	}
	if update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(want), 0o644)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("the wire declarations are missing (%w): run `%s` and commit the result", err, fix)
	}
	if string(got) != want {
		return fmt.Errorf("the committed %s does not match the Go structs: run `%s` and commit the result\n\n%s",
			path, fix, FirstDifference(string(got), want))
	}
	return nil
}

// FirstDifference names the first line where got and want differ: in a test
// log, the one line that moved is what the reader needs.
func FirstDifference(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return fmt.Sprintf("first difference at line %d:\n  committed: %q\n  generated: %q", i+1, gl, wl)
		}
	}
	return "(files differ only in length)"
}

type gen struct {
	seen      map[reflect.Type]bool
	queue     []reflect.Type
	names     map[string]reflect.Type // TypeScript name → the Go type declared under it
	conflicts []error
	shadowed  bool     // a type named Record is declared
	unknown   []string // Type.field declared unknown
	current   string   // the type being rendered, for unknown's message
}

// found queues t for declaration the first time it is reached.
func (g *gen) found(t reflect.Type) {
	if g.seen[t] {
		return
	}
	g.seen[t] = true
	g.queue = append(g.queue, t)
	name := tsName(t)
	if prev, ok := g.names[name]; ok {
		g.conflicts = append(g.conflicts, fmt.Errorf("wiregen: two Go types are TypeScript %q: %s and %s", name, qualified(prev), qualified(t)))
		return
	}
	g.names[name] = t
}

func qualified(t reflect.Type) string { return t.PkgPath() + "." + t.Name() }

func (g *gen) render(t reflect.Type) string {
	if values, ok := enumValues(t); ok {
		return renderUnion(t, values)
	}
	g.current = tsName(t)
	var b strings.Builder
	fmt.Fprintf(&b, "export interface %s {\n", tsName(t))
	g.fields(&b, t)
	b.WriteString("}\n")
	return b.String()
}

// fields writes t's fields as encoding/json writes them.
func (g *gen) fields(b *strings.Builder, t reflect.Type) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, opts := jsonName(f)
		if name == "-" {
			continue
		}
		if f.Anonymous && f.Tag.Get("json") == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				g.fields(b, ft) // promoted, even from an unexported embedded type
				continue
			}
		}
		if f.PkgPath != "" {
			continue // unexported: encoding/json never writes it
		}
		typ := g.tsType(f.Type, name)
		optional := ""
		if opts.omitempty {
			optional = "?" // absent when zero, never null
		} else if nullable(f.Type) {
			typ += " | null" // a nil slice, map or pointer is JSON null
		}
		fmt.Fprintf(b, "  %s%s: %s;\n", name, optional, typ)
	}
}

func nullable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Slice, reflect.Map, reflect.Pointer, reflect.Interface:
		return true
	}
	return false
}

var (
	timeType          = reflect.TypeOf(time.Time{})
	rawMessageType    = reflect.TypeOf(json.RawMessage(nil))
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// implements is whether t or *t has the method set of iface: encoding/json
// calls a pointer-receiver method on an addressable value.
func implements(t, iface reflect.Type) bool {
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}

func (g *gen) tsType(t reflect.Type, field string) string {
	if t.Kind() == reflect.Pointer {
		return g.tsType(t.Elem(), field)
	}
	if t == timeType {
		return "string"
	}
	if t == rawMessageType {
		g.unknown = append(g.unknown, g.current+"."+field+" (json.RawMessage)")
		return "unknown"
	}
	if _, ok := enumValues(t); ok {
		g.found(t)
		return tsName(t)
	}
	if implements(t, jsonMarshalerType) {
		g.unknown = append(g.unknown, fmt.Sprintf("%s.%s (%s implements json.Marshaler)", g.current, field, t))
		return "unknown"
	}
	if implements(t, textMarshalerType) {
		return "string"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		if e := t.Elem(); t.Kind() == reflect.Slice && e.Kind() == reflect.Uint8 &&
			!implements(e, jsonMarshalerType) && !implements(e, textMarshalerType) {
			return "string" // base64, as encoding/json writes []byte
		}
		return g.tsType(t.Elem(), field) + "[]"
	case reflect.Map:
		k, v := g.tsType(t.Key(), field), g.tsType(t.Elem(), field)
		if g.shadowed {
			return fmt.Sprintf("{ [key: %s]: %s }", k, v)
		}
		return fmt.Sprintf("Record<%s, %s>", k, v)
	case reflect.Struct:
		if t.Name() != "" {
			g.found(t)
			return tsName(t)
		}
	}
	// unknown, never any: the page must narrow it before use
	g.unknown = append(g.unknown, g.current+"."+field)
	return "unknown"
}

// enumValues is the values of a named string type with a Values() method.
func enumValues(t reflect.Type) ([]string, bool) {
	if t.Kind() != reflect.String || t.Name() == "" || t.PkgPath() == "" {
		return nil, false
	}
	m, ok := t.MethodByName("Values")
	if !ok || m.Type.NumIn() != 1 || m.Type.NumOut() != 1 || m.Type.Out(0) != reflect.TypeOf([]string(nil)) {
		return nil, false
	}
	return m.Func.Call([]reflect.Value{reflect.Zero(t)})[0].Interface().([]string), true
}

func renderUnion(t reflect.Type, values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "'" + strings.ReplaceAll(v, "'", `\'`) + "'"
	}
	line := fmt.Sprintf("export type %s = %s;\n", tsName(t), strings.Join(quoted, " | "))
	if len(line) <= 81 {
		return line
	}
	// Past 80 columns, one value per line, the shape prettier writes.
	return fmt.Sprintf("export type %s =\n  | %s;\n", tsName(t), strings.Join(quoted, "\n  | "))
}

// tsName is the Go type's name with an exported first letter: a view struct
// is unexported in Go, and the page has no such notion.
func tsName(t reflect.Type) string {
	if t == reflect.TypeOf(msgstate.State("")) {
		return "MessageState"
	}
	n := t.Name()
	if n == "" {
		return "unknown"
	}
	return strings.ToUpper(n[:1]) + n[1:]
}

type jsonOpts struct{ omitempty bool }

func jsonName(f reflect.StructField) (string, jsonOpts) {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name, jsonOpts{}
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = f.Name
	}
	var o jsonOpts
	for _, p := range parts[1:] {
		if p == "omitempty" || p == "omitzero" {
			o.omitempty = true
		}
	}
	return name, o
}
