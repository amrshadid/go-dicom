package attributes_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader/attributes"
)

// failingHandler is a slog handler that fails the test on every record: a
// write to the global logger where none is wanted.
type failingHandler struct{ t *testing.T }

func (h failingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h failingHandler) Handle(_ context.Context, r slog.Record) error {
	h.t.Errorf("the global logger was written to: %s %s", r.Level, r.Message)
	return nil
}
func (h failingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h failingHandler) WithGroup(string) slog.Handler      { return h }

// decodeAll decodes the text of every element of elements, items of
// sequences under the encodings they inherit, through ctx's logger, and
// returns how many values were decoded.
func decodeAll(ctx context.Context, elements []*attributes.DataElementValue, inherited []string) int {
	encodings := attributes.TextEncodingsWithContext(ctx, elements, inherited)
	n := 0
	for _, e := range elements {
		for _, item := range e.Items {
			n += decodeAll(ctx, item.Elements, encodings)
		}
		if len(e.Value) > 0 {
			_ = attributes.DecodeTextWithContext(ctx, e.VR, e.Value, encodings)
			n++
		}
	}
	return n
}

// TestTextIsDecodedToTheReadsLogger proves that a caller who gave a read a
// logger of its own gets nothing on the global one from decoding that read's
// text either. TextEncodings and DecodeText take no logger, so what the
// charset package logs while decoding, an unknown or unsupported Specific
// Character Set or a value that does not decode, went to config.Logger
// whatever logger the read had. Every corpus file is read, the charset
// files with them, and every value of each decoded, with config.Logger
// failing the test on any write; the lines go to the read's logger.
func TestTextIsDecodedToTheReadsLogger(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	var files []string
	for _, pattern := range []string{"*.dcm", filepath.Join("..", "charset_files", "*.dcm")} {
		found, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, found...)
	}
	if len(files) < 50 {
		t.Fatalf("%d corpus files in %s", len(files), dir)
	}
	// No corpus file declares a character set this library does not know,
	// so two are made from corpus files: a misspelled ISO_IR 100, which is
	// read as what it means with a warning, and an unknown term, which is
	// read as the default repertoire with a warning (PS3.5 6.1.2.5.3).
	for _, v := range []struct{ from, old, new string }{
		{"chrFren.dcm", "ISO_IR 100", "ISO IR 100"},
		{"chrRuss.dcm", "ISO_IR 144", "ISO_IR 999"},
	} {
		data, err := os.ReadFile(filepath.Join(dir, "..", "charset_files", v.from))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Count(data, []byte(v.old)) != 1 {
			t.Fatalf("%s does not declare %s once", v.from, v.old)
		}
		path := filepath.Join(t.TempDir(), v.from)
		if err := os.WriteFile(path, bytes.Replace(data, []byte(v.old), []byte(v.new), 1), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
	}
	previous := config.Logger
	config.Logger = slog.New(failingHandler{t})
	t.Cleanup(func() { config.Logger = previous })

	// The read and the decoding log to loggers of their own here, so that
	// what decoding logs is seen to arrive where it is sent.
	var readLog, decodeLog bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&readLog, nil))
	ctx := config.WithLogger(context.Background(), slog.New(slog.NewTextHandler(&decodeLog, nil)))
	read, values := 0, 0
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := attributes.Read(filebase.NewFileReader(f), attributes.ReadOptions{Logger: logger})
		_ = f.Close()
		if err != nil {
			continue
		}
		read++
		values += decodeAll(ctx, file.DataElements, nil)
	}
	if read < 50 {
		t.Errorf("only %d files read", read)
	}
	for _, want := range []string{"Incorrect value for Specific Character Set", "Unknown encoding"} {
		if !strings.Contains(decodeLog.String(), want) {
			t.Errorf("decoding did not log %q to the logger it was given", want)
		}
	}
	t.Logf("%d files, %d values decoded; decoding logged:\n%s", read, values, decodeLog.String())
}
