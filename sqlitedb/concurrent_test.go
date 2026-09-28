package sqlitedb

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// TestHelperOpen is run as a child process by TestConcurrentFirstOpen.
func TestHelperOpen(t *testing.T) {
	p := os.Getenv("SQLITEDB_HELPER_PATH")
	if p == "" {
		t.Skip("helper")
	}
	d, err := Open(ctx, p, Options{Migrations: []Step{stepA, stepB}})
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
}

// Several processes of one tool opening a brand-new database at once must all
// succeed: exactly one runs each step, the others see it done.
func TestConcurrentFirstOpen(t *testing.T) {
	for round := 0; round < 5; round++ {
		p := filepath.Join(t.TempDir(), "tool.db")
		var wg sync.WaitGroup
		errs := make(chan error, 6)
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cmd := exec.Command(os.Args[0], "-test.run=^TestHelperOpen$", "-test.count=1")
				cmd.Env = append(os.Environ(), "SQLITEDB_HELPER_PATH="+p)
				if out, err := cmd.CombinedOutput(); err != nil {
					errs <- fmt.Errorf("%v\n%s", err, out)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("round %d: concurrent open failed: %v", round, err)
		}
	}
}
