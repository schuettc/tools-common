// Package claudeplugin renders a family tool's Claude Code plugin: the
// manifest, the .mcp.json that runs `<bin> channel` for the tool's MCP tools,
// and a mod (hooks/hooks.json, hooks/register.js) that runs `<bin> watch` and
// starts a turn with each event it prints. Every tool's mod comes from the
// one template in this package; AssertCurrent keeps a tool's committed copy
// equal to it.
//
// plugin.json carries no "version": for a plugin in a git-hosted marketplace
// Claude Code then versions it by commit, so the committed plugin does not
// change on every release.
package claudeplugin

import (
	"bytes"
	_ "embed" // registerJS
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"text/template"
)

//go:embed template/register.js
var registerJS string

// Spec is what differs between tools' plugins. Name, Bin and ServerName are
// required; the rest go into plugin.json when set.
type Spec struct {
	// Name is the plugin's name (kebab-case) and the first word of each
	// event Claude reads ("galley event ...").
	Name        string
	DisplayName string
	// Bin is the executable: `<Bin> channel` serves the MCP tools and
	// `<Bin> watch` prints events.
	Bin string
	// ServerName is the MCP server's key in .mcp.json.
	ServerName  string
	Description string
	Author      string
	AuthorURL   string
	Homepage    string
	Repository  string
	License     string
	Keywords    []string
}

// Files is the rendered plugin, by path relative to the plugin directory.
type Files map[string][]byte

// Render returns the plugin's files for s.
func Render(s Spec) (Files, error) {
	if s.Name == "" || s.Bin == "" || s.ServerName == "" {
		return nil, errors.New("claudeplugin: Spec needs Name, Bin and ServerName")
	}
	type author struct {
		Name string `json:"name"`
		URL  string `json:"url,omitempty"`
	}
	manifest := struct {
		Name        string   `json:"name"`
		DisplayName string   `json:"displayName,omitempty"`
		Description string   `json:"description,omitempty"`
		Author      *author  `json:"author,omitempty"`
		Homepage    string   `json:"homepage,omitempty"`
		Repository  string   `json:"repository,omitempty"`
		License     string   `json:"license,omitempty"`
		Keywords    []string `json:"keywords,omitempty"`
		MCPServers  string   `json:"mcpServers"`
	}{s.Name, s.DisplayName, s.Description, nil, s.Homepage, s.Repository, s.License, s.Keywords, "./.mcp.json"}
	if s.Author != "" {
		manifest.Author = &author{s.Author, s.AuthorURL}
	}
	type server struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	mcp := map[string]map[string]server{"mcpServers": {s.ServerName: {s.Bin, []string{"channel"}}}}

	files := Files{}
	var err error
	if files[".claude-plugin/plugin.json"], err = indent(manifest); err != nil {
		return nil, err
	}
	if files[".mcp.json"], err = indent(mcp); err != nil {
		return nil, err
	}
	hooks := struct {
		Description string   `json:"description"`
		Modules     []string `json:"modules"`
	}{"Starts a turn with each " + s.Name + " event", []string{"./register.js"}}
	if files["hooks/hooks.json"], err = indent(hooks); err != nil {
		return nil, err
	}
	if files["hooks/register.js"], err = renderJS(registerJS, s); err != nil {
		return nil, err
	}
	return files, nil
}

// renderJS fills a register.js template with s's name and binary.
func renderJS(src string, s Spec) ([]byte, error) {
	nameJS, _ := json.Marshal(s.Name)
	binJS, _ := json.Marshal(s.Bin)
	t, err := template.New("register.js").Parse(src)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, map[string]string{"NameJS": string(nameJS), "BinJS": string(binJS)}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func indent(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	return append(b, '\n'), err
}

// Write renders s into dir, creating directories as needed. Files already in
// dir that the plugin does not render (a skills directory) are left alone.
func Write(dir string, s Spec) error {
	files, err := Render(s)
	if err != nil {
		return err
	}
	for _, p := range files.paths() {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[p], 0o644); err != nil { //nolint:gosec // plugin files are meant to be read by everyone
			return err
		}
	}
	return nil
}

func (f Files) paths() []string {
	ps := make([]string, 0, len(f))
	for p := range f {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps
}
