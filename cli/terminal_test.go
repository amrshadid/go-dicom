package cli

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The library's logger wrote to the terminal a command was using, in its own
// format, among the command's output: over pydicom's corpus, show, info,
// convert and codify each printed 40 such lines (#283).

// oddFile is an Explicit VR Little Endian file holding one element written
// without a VR, which the reader copes with and logs a warning about.
func oddFile(t *testing.T) string {
	t.Helper()
	const syntax = "1.2.840.10008.1.2.1\x00"
	var meta bytes.Buffer
	meta.Write([]byte{0x02, 0x00, 0x10, 0x00, 'U', 'I', byte(len(syntax)), 0})
	meta.WriteString(syntax)

	var f bytes.Buffer
	f.Write(make([]byte, 128))
	f.WriteString("DICM")
	f.Write([]byte{0x02, 0x00, 0x00, 0x00, 'U', 'L', 4, 0})
	f.Write(binary.LittleEndian.AppendUint32(nil, uint32(meta.Len())))
	f.Write(meta.Bytes())
	f.Write([]byte{0x08, 0x00, 0x60, 0x00, 'C', 'S', 2, 0, 'C', 'T'})
	// (0010,0020) Patient ID with a 4-byte length where a VR belongs.
	f.Write([]byte{0x10, 0x00, 0x20, 0x00, 4, 0, 0, 0, 'I', 'D', '0', '1'})

	path := filepath.Join(t.TempDir(), "odd.dcm")
	if err := os.WriteFile(path, f.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCommandsOwnTheirTerminal(t *testing.T) {
	binary := buildCLI(t)
	path := oddFile(t)

	run := func(env string, args ...string) (stdout, stderr string) {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), "GODICOM_LOG="+env)
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, errOut.String())
		}
		return out.String(), errOut.String()
	}

	for _, args := range [][]string{
		{"show", path},
		{"info", path},
		{"show", path, "--json"},
		{"convert", path, filepath.Join(t.TempDir(), "out.json")},
	} {
		stdout, stderr := run("", args...)
		if strings.Contains(stdout+stderr, "level=") {
			t.Errorf("%v prints the library's log:\n%s%s", args[:1], stdout, stderr)
		}
	}
	// The element is read all the same.
	if stdout, _ := run("", "show", path); !strings.Contains(stdout, "ID01") {
		t.Errorf("show does not list the oddly written element:\n%s", stdout)
	}

	// Asked for, the log is there: it is how such a file is diagnosed.
	if _, stderr := run("warn", "show", path); !strings.Contains(stderr, "level=WARN") {
		t.Errorf("GODICOM_LOG=warn prints no warning:\n%s", stderr)
	}
}
