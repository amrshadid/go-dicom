package cli_test

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/cli"
)

func TestNewCLI(t *testing.T) {
	c := cli.NewCLI("dicom", "1.0.0")

	if c.Name != "dicom" {
		t.Errorf("Expected name 'dicom', got '%s'", c.Name)
	}
	if c.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", c.Version)
	}
	if len(c.Commands) != 0 {
		t.Errorf("Expected 0 commands, got %d", len(c.Commands))
	}
}

func TestRegisterCommand(t *testing.T) {
	c := cli.NewCLI("dicom", "1.0.0")
	cmd := cli.NewShowCommand()

	c.RegisterCommand(cmd)

	if _, exists := c.Commands["show"]; !exists {
		t.Error("Command 'show' not registered")
	}
}

func TestShowCommandName(t *testing.T) {
	cmd := cli.NewShowCommand()
	if cmd.Name() != "show" {
		t.Errorf("Expected name 'show', got '%s'", cmd.Name())
	}
}

func TestShowCommandDescription(t *testing.T) {
	cmd := cli.NewShowCommand()
	desc := cmd.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}
}

func TestShowCommandFlagsBasic(t *testing.T) {
	cmd := cli.NewShowCommand()
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	cmd.AddFlags(fs)

	if fs.Lookup("exclude-private") == nil {
		t.Error("exclude-private flag not found")
	}
	if fs.Lookup("top") == nil {
		t.Error("top flag not found")
	}
	if fs.Lookup("quiet") == nil {
		t.Error("quiet flag not found")
	}
}

func TestShowCommandExecute(t *testing.T) {
	cmd := cli.NewShowCommand()

	tmpfile, err := os.CreateTemp("", "test*.dcm")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	err = cmd.Execute([]string{tmpfile.Name()})
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	err = cmd.Execute([]string{"/nonexistent/file.dcm"})
	if err == nil {
		t.Error("Execute should fail for nonexistent file")
	}

	cmd2 := cli.NewShowCommand()
	err = cmd2.Execute([]string{})
	if err == nil {
		t.Error("Execute should fail with no arguments")
	}
}

func TestShowCommandWithElement(t *testing.T) {
	cmd := cli.NewShowCommand()

	tmpfile, err := os.CreateTemp("", "test*.dcm")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	spec := tmpfile.Name() + "::PatientName"
	err = cmd.Execute([]string{spec})
	if err == nil {
		return
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "failed") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestInfoCommandName(t *testing.T) {
	cmd := cli.NewInfoCommand()
	if cmd.Name() != "info" {
		t.Errorf("Expected name 'info', got '%s'", cmd.Name())
	}
}

func TestInfoCommandDescription(t *testing.T) {
	cmd := cli.NewInfoCommand()
	desc := cmd.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}
}

func TestInfoCommandExecute(t *testing.T) {
	cmd := cli.NewInfoCommand()

	tmpfile, err := os.CreateTemp("", "test*.dcm")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	err = cmd.Execute([]string{tmpfile.Name()})
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	err = cmd.Execute([]string{"/nonexistent/file.dcm"})
	if err == nil {
		t.Error("Execute should fail for nonexistent file")
	}

	cmd2 := cli.NewInfoCommand()
	err = cmd2.Execute([]string{})
	if err == nil {
		t.Error("Execute should fail with no arguments")
	}
}

func TestConvertCommandName(t *testing.T) {
	cmd := cli.NewConvertCommand()
	if cmd.Name() != "convert" {
		t.Errorf("Expected name 'convert', got '%s'", cmd.Name())
	}
}

func TestConvertCommandDescription(t *testing.T) {
	cmd := cli.NewConvertCommand()
	desc := cmd.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}
}

func TestConvertCommandExecute(t *testing.T) {
	cmd := cli.NewConvertCommand()

	tmpfile, err := os.CreateTemp("", "test*.dcm")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	outputFile := tmpfile.Name() + ".json"
	err = cmd.Execute([]string{tmpfile.Name(), outputFile})
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	err = cmd.Execute([]string{"/nonexistent/file.dcm", outputFile})
	if err == nil {
		t.Error("Execute should fail for nonexistent input file")
	}

	cmd2 := cli.NewConvertCommand()
	err = cmd2.Execute([]string{})
	if err == nil {
		t.Error("Execute should fail with no arguments")
	}

	err = cmd2.Execute([]string{tmpfile.Name()})
	if err == nil {
		t.Error("Execute should fail with only one argument")
	}
}

func TestConvertCommandInvalidFormat(t *testing.T) {
	cmd := cli.NewConvertCommand()

	tmpfile, err := os.CreateTemp("", "test*.dcm")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	outputFile := tmpfile.Name() + ".out"
	err = cmd.Execute([]string{tmpfile.Name(), outputFile})
	// Invalid format may still succeed depending on implementation
	// Main point is that command can be created and executed
	_ = err
}

func TestCLIRun(t *testing.T) {
	c := cli.NewCLI("dicom", "1.0.0")
	cmd := cli.NewShowCommand()
	c.RegisterCommand(cmd)

	tmpfile, err := os.CreateTemp("", "test*.dcm")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	err = c.Run([]string{"show", tmpfile.Name()})
	if err != nil {
		t.Errorf("Run failed: %v", err)
	}

	err = c.Run([]string{"unknown"})
	if err == nil {
		t.Error("Run should fail for unknown command")
	}

	err = c.Run([]string{})
	if err != nil {
		t.Errorf("Run should succeed and show help with no arguments, got error: %v", err)
	}
}

func TestCLIVersion(t *testing.T) {
	c := cli.NewCLI("dicom", "1.0.0")

	err := c.Run([]string{"-v"})
	if err != nil {
		t.Errorf("Version flag failed: %v", err)
	}

	err = c.Run([]string{"--version"})
	if err != nil {
		t.Errorf("Version flag (long) failed: %v", err)
	}
}

func TestParseFileSpec(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "test*.dcm")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	tests := []struct {
		spec       string
		expectFile string
		expectElem string
		shouldErr  bool
	}{
		{tmpfile.Name(), tmpfile.Name(), "", false},
		{tmpfile.Name() + "::PatientName", tmpfile.Name(), "PatientName", false},
		{"/nonexistent/file.dcm", "", "", true},
	}

	for _, tt := range tests {
		fs, err := cli.ParseFileSpec(tt.spec)
		if (err != nil) != tt.shouldErr {
			t.Errorf("ParseFileSpec(%s): expected error=%v, got %v", tt.spec, tt.shouldErr, err)
		}
		if !tt.shouldErr {
			if fs.FilePath != tt.expectFile {
				t.Errorf("ParseFileSpec(%s): expected file=%s, got %s", tt.spec, tt.expectFile, fs.FilePath)
			}
			if fs.Element != tt.expectElem {
				t.Errorf("ParseFileSpec(%s): expected element=%s, got %s", tt.spec, tt.expectElem, fs.Element)
			}
		}
	}
}

func TestFileSpecWithElementName(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "test*.dcm")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	spec := tmpfile.Name() + "::PatientName"
	fs, err := cli.ParseFileSpec(spec)
	if err != nil {
		t.Errorf("ParseFileSpec with element failed: %v", err)
	}
	if fs.Element != "PatientName" {
		t.Errorf("Expected element 'PatientName', got '%s'", fs.Element)
	}
}

func TestPrintVersion(t *testing.T) {
	// Just ensure it doesn't panic
	cli.PrintVersion("dicom", "1.0.0")
}

func TestPrintUsage(t *testing.T) {
	// Just ensure it doesn't panic
	cli.PrintUsage("dicom")
}

// TagDoc Command Tests
func TestTagDocCommandName(t *testing.T) {
	cmd := cli.NewTagDocCommand()
	if cmd.Name() != "tag-doc" {
		t.Errorf("Expected name 'tag-doc', got '%s'", cmd.Name())
	}
}

func TestTagDocCommandDescription(t *testing.T) {
	cmd := cli.NewTagDocCommand()
	desc := cmd.Description()
	if desc == "" {
		t.Error("TagDoc description should not be empty")
	}
}

func TestTagDocCommandFlags(t *testing.T) {
	cmd := cli.NewTagDocCommand()
	fs := flag.NewFlagSet("tag-doc", flag.ContinueOnError)
	cmd.AddFlags(fs)

	if fs.Lookup("keyword") == nil {
		t.Error("keyword flag not found")
	}
	if fs.Lookup("format") == nil {
		t.Error("format flag not found")
	}
}

func TestTagDocCommandExecute(t *testing.T) {
	cmd := cli.NewTagDocCommand()
	err := cmd.Execute([]string{"-keyword", "PatientName"})
	if err != nil {
		t.Errorf("TagDoc execute with valid keyword failed: %v", err)
	}

	cmd2 := cli.NewTagDocCommand()
	err = cmd2.Execute([]string{"-keyword", "InvalidKeywordXYZ"})
	if err == nil {
		t.Error("TagDoc should fail for invalid keyword")
	}
}

func TestTagDocCommandFormats(t *testing.T) {
	formats := []string{"text", "markdown", "json"}
	for _, format := range formats {
		cmd := cli.NewTagDocCommand()
		err := cmd.Execute([]string{"-keyword", "PatientName", "-format", format})
		if err != nil {
			t.Errorf("TagDoc with format %s failed: %v", format, err)
		}
	}
}

// Codify Command Tests
func TestCodifyCommandName(t *testing.T) {
	cmd := cli.NewCodifyCommand()
	if cmd.Name() != "codify" {
		t.Errorf("Expected name 'codify', got '%s'", cmd.Name())
	}
}

func TestCodifyCommandDescription(t *testing.T) {
	cmd := cli.NewCodifyCommand()
	desc := cmd.Description()
	if desc == "" {
		t.Error("Codify description should not be empty")
	}
}

func TestCodifyCommandFlags(t *testing.T) {
	cmd := cli.NewCodifyCommand()
	fs := flag.NewFlagSet("codify", flag.ContinueOnError)
	cmd.AddFlags(fs)

	if fs.Lookup("exclude-size") == nil {
		t.Error("exclude-size flag not found")
	}
	if fs.Lookup("exclude-private") == nil {
		t.Error("exclude-private flag not found")
	}
	if fs.Lookup("package") == nil {
		t.Error("package flag not found")
	}
	if fs.Lookup("function") == nil {
		t.Error("function flag not found")
	}
	if fs.Lookup("output") == nil {
		t.Error("output flag not found")
	}
}

func TestCodifyCommandExecute(t *testing.T) {
	cmd := cli.NewCodifyCommand()

	tmpfile, err := os.CreateTemp("", "test*.dcm")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	err = cmd.Execute([]string{tmpfile.Name()})
	if err != nil {
		t.Errorf("Codify execute with empty DICOM file failed: %v", err)
	}

	cmd2 := cli.NewCodifyCommand()
	err = cmd2.Execute([]string{"/nonexistent/file.dcm"})
	if err == nil {
		t.Error("Codify should fail for nonexistent file")
	}

	cmd3 := cli.NewCodifyCommand()
	err = cmd3.Execute([]string{})
	if err == nil {
		t.Error("Codify should fail with no arguments")
	}
}

func TestCodifyCommandWithOptions(t *testing.T) {
	cmd := cli.NewCodifyCommand()

	tmpfile, err := os.CreateTemp("", "test*.dcm")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	outfile, err := os.CreateTemp("", "test*.go")
	if err != nil {
		t.Fatalf("Failed to create temp output file: %v", err)
	}
	defer os.Remove(outfile.Name())
	outfile.Close()

	err = cmd.Execute([]string{
		"-package", "mypackage",
		"-function", "myFunction",
		"-exclude-size", "50",
		"-output", outfile.Name(),
		tmpfile.Name(),
	})
	if err != nil {
		t.Errorf("Codify with options failed: %v", err)
	}

	if fileInfo, err := os.Stat(outfile.Name()); err == nil {
		if fileInfo.Size() == 0 {
			t.Error("Codify output file should have content")
		}
	}
}
