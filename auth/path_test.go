package auth_test

import (
	"path/filepath"
	"testing"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/auth"
)

// auth can't import tools (tools registers login from auth), so it keeps
// its own copy of ConfigDir's rules; this holds the two together.
func TestTheTokenIsInSubaudsConfigDir(t *testing.T) {
	for _, env := range []map[string]string{
		{"SUBAUD_HOME": "/h/subaud", "XDG_CONFIG_HOME": "/x"},
		{"SUBAUD_HOME": "relative", "XDG_CONFIG_HOME": "/x"},
		{"SUBAUD_HOME": "", "XDG_CONFIG_HOME": "relative", "HOME": "/home/court"},
		{"SUBAUD_HOME": "", "XDG_CONFIG_HOME": "", "HOME": "/home/court"},
	} {
		for k, v := range env {
			t.Setenv(k, v)
		}
		if got, want := auth.TokenPath(), filepath.Join(tools.ConfigDir("subaud"), "token"); got != want {
			t.Fatalf("%v: TokenPath() = %s, want %s", env, got, want)
		}
	}
}
