package filereader

import (
	"bytes"
	"context"
	"encoding/binary"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/filebase"
)

// A read logged only to the global config.Logger, so a server could not give
// one read a logger of its own, or keep one quiet, without changing every
// other read in the process; and the character set lines printed the file's
// own Specific Character Set text (#481).

// recorder is a logger that keeps its lines, without their time.
type recorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func (r *recorder) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(r, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
}

// refusing fails the test on any line written to it.
type refusing struct{ t *testing.T }

func (h refusing) Enabled(context.Context, slog.Level) bool { return true }
func (h refusing) Handle(_ context.Context, r slog.Record) error {
	h.t.Errorf("the global logger was written to: %s", r.Message)
	return nil
}
func (h refusing) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h refusing) WithGroup(string) slog.Handler      { return h }

// setGlobalLogger replaces config.Logger for the test.
func setGlobalLogger(t *testing.T, logger *slog.Logger) {
	t.Helper()
	saved := config.Logger
	config.Logger = logger
	t.Cleanup(func() { config.Logger = saved })
}

// specificCharacterSet is a file whose only text is under the given
// Specific Character Set, with a retired attribute that validation reports.
func specificCharacterSet(term string) []byte {
	var ds bytes.Buffer
	if len(term)%2 == 1 {
		term += " "
	}
	ds.Write(elementHeader(0x0008, 0x0005, "CS", uint32(len(term))))
	ds.WriteString(term)
	ds.Write(elementHeader(0x0010, 0x0010, "PN", 6))
	ds.Write([]byte{'C', 'a', 'f', 0xE9, ' ', ' '})
	ds.Write(elementHeader(0x0010, 0x1000, "LO", 4)) // Other Patient IDs, retired
	ds.WriteString("1234")
	return part10("1.2.840.10008.1.2.1", ds.Bytes())
}

// elementHeader is an explicit VR little endian element header.
func elementHeader(group, element uint16, vr string, length uint32) []byte {
	out := binary.LittleEndian.AppendUint16(nil, group)
	out = binary.LittleEndian.AppendUint16(out, element)
	out = append(out, vr...)
	if isShortVR(vr) {
		return binary.LittleEndian.AppendUint16(out, uint16(length))
	}
	return binary.LittleEndian.AppendUint32(append(out, 0, 0), length)
}

// loggedFiles are files whose reading logs: a text file, a file cut short,
// two Specific Character Sets that are not defined terms, and a meta group
// without its group length.
func loggedFiles() map[string][]byte {
	cut := bulkFile()
	noGroupLength := part10("1.2.840.10008.1.2.1", elementHeader(0x0010, 0x0020, "LO", 0))
	noGroupLength = append(noGroupLength[:132:132], noGroupLength[144:]...)
	return map[string][]byte{
		"text":                 []byte("this is a plain text file, not DICOM at all.\nIt has a second line.\n"),
		"truncated":            cut[:len(cut)-4],
		"misspelled term":      specificCharacterSet("ISO IR 100"),
		"unknown term":         specificCharacterSet("Jane Doe 555-0100"),
		"no meta group length": noGroupLength,
	}
}

// readAll reads data with options and does everything a program does with
// the result that may log: the data set, the character set, the frames.
func readAll(data []byte, options ReadOptions) {
	df, err := ReadDICOMFileWithOptions(filebase.NewFileReader(bytes.NewReader(data)), options)
	if err != nil {
		return
	}
	_ = df.GetDataset()
	_ = df.GetCharacterSet()
	_, _ = df.FrameRanges(bytes.NewReader(data))
}

// corpusFiles returns pydicom's corpus, or nothing when it is not set.
func corpusFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		return files
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	more, _ := filepath.Glob(filepath.Join(dir, "*", "*.dcm"))
	for _, path := range append(paths, more...) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[filepath.Base(path)] = data
	}
	return files
}

// Given a logger, a read writes nothing to the global one: not the reader,
// not the character sets, not the validation GetDataset runs, across every
// file of the corpus and the files that log the most.
func TestAReadWithItsOwnLoggerLeavesTheGlobalOneAlone(t *testing.T) {
	setGlobalLogger(t, slog.New(refusing{t}))
	files := corpusFiles(t)
	for name, data := range loggedFiles() {
		files[name] = data
	}
	var own recorder
	for _, data := range files {
		readAll(data, ReadOptions{Logger: own.logger()})
		readAll(data, ReadOptions{Logger: own.logger(), SkipBulkData: true})
	}
	lines := own.String()
	for _, want := range []string{
		"file is truncated", "(6874,7369)", "module=charset", "semantic validation", "no group length",
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("the read's own logger has no line with %q:\n%s", want, lines)
		}
	}
}

// What a read logs names tags, VRs, lengths and offsets, and never the
// text of a value: a Specific Character Set that is not a defined term is
// the file's own text.
func TestALogLineCarriesNoTextFromTheFile(t *testing.T) {
	for _, term := range []string{"Jane Doe 555-0100", "ISO IR 100"} {
		var own recorder
		readAll(specificCharacterSet(term), ReadOptions{Logger: own.logger()})
		lines := own.String()
		if strings.Contains(lines, strings.TrimSpace(term)) || strings.Contains(lines, "Caf") {
			t.Errorf("a line repeats the file's text:\n%s", lines)
		}
		if !strings.Contains(lines, "module=charset") {
			t.Errorf("nothing was logged about %q:\n%s", term, lines)
		}
	}
}

// Two reads at once, with a logger each, each log exactly what they log
// alone, and nothing of the other's.
func TestTwoReadsAtOnceLogToTheirOwnLoggers(t *testing.T) {
	setGlobalLogger(t, slog.New(refusing{t}))
	files := loggedFiles()
	a, b := files["text"], files["unknown term"]
	alone := func(data []byte) string {
		var r recorder
		readAll(data, ReadOptions{Logger: r.logger()})
		return r.String()
	}
	wantA, wantB := alone(a), alone(b)
	if wantA == "" || wantB == "" || wantA == wantB {
		t.Fatalf("the two files must log, and differently:\n%s\n%s", wantA, wantB)
	}

	const rounds = 50
	var ra, rb recorder
	var wg sync.WaitGroup
	for _, job := range []struct {
		data []byte
		into *recorder
	}{{a, &ra}, {b, &rb}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger := job.into.logger()
			for range rounds {
				readAll(job.data, ReadOptions{Logger: logger})
			}
		}()
	}
	wg.Wait()
	if got := ra.String(); got != strings.Repeat(wantA, rounds) {
		t.Errorf("the first read's logger has lines it did not log alone:\n%s", got)
	}
	if got := rb.String(); got != strings.Repeat(wantB, rounds) {
		t.Errorf("the second read's logger has lines it did not log alone:\n%s", got)
	}
}

// Without a logger a read logs to config.Logger, as it always did.
func TestAReadWithoutALoggerLogsToTheGlobalOne(t *testing.T) {
	var global recorder
	setGlobalLogger(t, global.logger())
	readAll(loggedFiles()["text"], ReadOptions{})
	if !strings.Contains(global.String(), "file is truncated") {
		t.Errorf("the global logger has %q", global.String())
	}
}

// silentReadChild is set in the process TestASilentReadWritesNothingToStderr
// starts, to say which logger the reads in it are given.
const silentReadChild = "GODICOM_SILENT_READ_CHILD"

// A read given a logger that discards writes nothing to stderr, across every
// file of the corpus and the files that log the most. The reads run in a
// process of their own, whose stderr is the one the default logger writes
// to; a second run with no logger shows the stderr is being watched.
func TestASilentReadWritesNothingToStderr(t *testing.T) {
	if mode := os.Getenv(silentReadChild); mode != "" {
		files := corpusFiles(t)
		for name, data := range loggedFiles() {
			files[name] = data
		}
		options := ReadOptions{}
		if mode == "discard" {
			options.Logger = slog.New(slog.DiscardHandler)
		}
		for _, data := range files {
			readAll(data, options)
		}
		t.Logf("read %d files", len(files))
		return
	}
	run := func(mode string) (stdout, stderr string) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestASilentReadWritesNothingToStderr$", "-test.v")
		cmd.Env = append(os.Environ(), silentReadChild+"="+mode)
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s: %v\n%s%s", mode, err, out.String(), errOut.String())
		}
		return out.String(), errOut.String()
	}
	stdout, stderr := run("discard")
	if !strings.Contains(stdout, "read ") || stderr != "" {
		t.Errorf("a silent read wrote to stderr:\n%s\nstdout:\n%s", stderr, stdout)
	}
	if _, stderr := run("global"); !strings.Contains(stderr, "file is truncated") {
		t.Errorf("without a logger nothing reached stderr, so it is not being watched:\n%s", stderr)
	}
}
