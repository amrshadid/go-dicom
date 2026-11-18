package network_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dcmtkTool returns the path of a dcmtk program: from DCMTK_BIN when it is
// set, otherwise from PATH. pynetdicom's console scripts share dcmtk's names,
// so a program found on PATH is checked to be dcmtk's. Absent, the test is
// skipped, or fails where GODICOM_REQUIRE_INTEROP is set.
func dcmtkTool(t *testing.T, name string) string {
	t.Helper()
	path := name
	if dir := os.Getenv("DCMTK_BIN"); dir != "" {
		path = filepath.Join(dir, name)
	}
	resolved, err := exec.LookPath(path)
	if err == nil {
		out, _ := exec.Command(resolved, "--version").CombinedOutput()
		if !strings.Contains(string(out), "dcmtk") {
			err = &exec.Error{Name: resolved, Err: exec.ErrNotFound}
		}
	}
	if err == nil {
		return resolved
	}
	if os.Getenv("GODICOM_REQUIRE_INTEROP") != "" {
		t.Fatalf("GODICOM_REQUIRE_INTEROP is set but dcmtk's %s is not available: %v", name, err)
	}
	t.Skipf("dcmtk's %s not available (%v); skipping third-party interoperability check", name, err)
	return ""
}
