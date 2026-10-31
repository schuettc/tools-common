package setupbundle

import (
	"embed"
	"flag"
	"io"
	"io/fs"
	"os"

	tools "github.com/subaud-tools/tools-common"
)

// Source is a tool's canonical embedded setup declaration and its scoped assets.
// Manifest is kept opaque. Files is rooted at the bundle's files/ directory;
// nil means no assets, never the working directory or the user's home.
type Source struct {
	Manifest []byte
	Files    fs.FS
}

// BundleCommand builds a release archive, without installing or discovering
// configuration. version is the default identity; release builds pass their
// exact version with --version, including when running an unstamped build.
func BundleCommand(tool, version string, source Source) tools.Command {
	const synopsis = "--version X.Y.Z --out PATH"
	const help = "Write the tool's embedded setup bundle only; do not install or configure anything."
	newFlags := func() *flag.FlagSet {
		f := flag.NewFlagSet("setup-bundle", flag.ContinueOnError)
		f.String("version", version, "exact release version")
		f.String("out", "", "output archive path")
		tools.SetUsage(f, tool+" setup-bundle "+synopsis, help)
		return f
	}
	return tools.Command{
		Name: "setup-bundle", Summary: "build the release setup archive", Synopsis: synopsis, Help: help, NewFlags: newFlags,
		Run: func(args []string, out, errw io.Writer) error {
			f := newFlags()
			if err := tools.ParseFlags(f, args, out); err != nil {
				return err
			}
			v, dest := f.Lookup("version").Value.String(), f.Lookup("out").Value.String()
			if f.NArg() != 0 || v == "" || dest == "" {
				return tools.UsageError{Msg: "setup-bundle requires a version and --out PATH, with no positional arguments"}
			}
			files := source.Files
			if files == nil {
				// A zero embed.FS has an empty root; no fallback filesystem is consulted.
				files = embed.FS{}
			}
			data, err := Build(tool, v, source.Manifest, files)
			if err != nil {
				return err
			}
			return os.WriteFile(dest, data, 0o644)
		},
	}
}
