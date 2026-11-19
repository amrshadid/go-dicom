package cli

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// scheduledExam is a worklist entry for one patient and one step.
func scheduledExam(t *testing.T, accession, patient, modality, station, date string) *dataset.Dataset {
	t.Helper()
	add := func(ds *dataset.Dataset, g, e uint16, vr dataelem.VR, v string) {
		if err := ds.Add(dataelem.NewDataElement(tag.New(g, e), vr, []byte(v))); err != nil {
			t.Fatal(err)
		}
	}
	entry := dataset.NewDataset()
	add(entry, 0x0008, 0x0050, dataelem.SH, accession)
	add(entry, 0x0010, 0x0010, dataelem.PN, patient)
	add(entry, 0x0010, 0x0020, dataelem.LO, "PID-"+accession)
	add(entry, 0x0020, 0x000D, dataelem.UI, "1.2.826.0.1.3680043.10.1."+accession)
	add(entry, 0x0040, 0x1001, dataelem.SH, "RP-"+accession)
	step := dataset.NewDataset()
	add(step, 0x0008, 0x0060, dataelem.CS, modality)
	add(step, 0x0040, 0x0001, dataelem.AE, station)
	add(step, 0x0040, 0x0002, dataelem.DA, date)
	add(step, 0x0040, 0x0003, dataelem.TM, "0930")
	add(step, 0x0040, 0x0009, dataelem.SH, "SPS-"+accession)
	add(step, 0x0040, 0x0012, dataelem.LO, "NONE")
	steps := sequence.New()
	if err := steps.Append(step); err != nil {
		t.Fatal(err)
	}
	if err := entry.AddSequence(tag.New(0x0040, 0x0100), steps); err != nil {
		t.Fatal(err)
	}
	return entry
}

func writeWorklistFile(t *testing.T, dir, name string, entry *dataset.Dataset) {
	t.Helper()
	if err := writeDICOMFile(filepath.Join(dir, name), "1.2.276.0.7230010.3.1.0.1",
		"1.2.826.0.1.3680043.10.2."+strconv.Itoa(len(name)), entry); err != nil {
		t.Fatal(err)
	}
}

// startWlmscp runs wlmscp over dir and returns its address and its output.
func startWlmscp(t *testing.T, binary, dir string) (string, *syncBuffer) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
	_ = probe.Close()

	server := exec.Command(binary, "wlmscp", "-dir", dir, "-port", port, "-aet", "WLM")
	out := &syncBuffer{}
	server.Stdout, server.Stderr = out, out
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })

	address := "127.0.0.1:" + port
	deadline := time.Now().Add(10 * time.Second)
	for {
		if conn, err := net.Dial("tcp", address); err == nil {
			_ = conn.Close()
			return address, out
		}
		if time.Now().After(deadline) {
			t.Fatalf("wlmscp never listened:\n%s", out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestWlmscpServesTheWorklistDirectory runs findscu -worklist against wlmscp
// over a directory of worklist files: a query finds the entries that match,
// with the scheduled step and the keys asked for; a file added while the
// server runs is served at the next query and a removed one is not; and a
// file that is not a worklist entry is not served, and is reported once
// however many queries read the directory.
func TestWlmscpServesTheWorklistDirectory(t *testing.T) {
	binary := buildCLI(t)
	dir := t.TempDir()
	writeWorklistFile(t, dir, "mr1.wl", scheduledExam(t, "A1", "VIVALDI^ANTONIO", "MR", "AA32", "20261118"))
	writeWorklistFile(t, dir, "ct1.dcm", scheduledExam(t, "A2", "HAYDN^FRANZ", "CT", "AB45", "20261119"))
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not DICOM\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An image from pydicom's corpus is DICOM and schedules nothing.
	image := ""
	if corpus := os.Getenv("GODICOM_PYDICOM_DATA"); corpus != "" {
		data, err := os.ReadFile(filepath.Join(corpus, "CT_small.dcm"))
		if err != nil {
			t.Fatal(err)
		}
		image = "CT_small.dcm"
		if err := os.WriteFile(filepath.Join(dir, image), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	address, serverOut := startWlmscp(t, binary, dir)

	findscu := func(args ...string) string {
		t.Helper()
		cmd := append([]string{"findscu", "-aec", "WLM", "-timeout", "10", "-worklist"}, args...)
		out, err := exec.Command(binary, append(cmd, address)...).CombinedOutput()
		if err != nil {
			t.Fatalf("findscu %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	out := findscu("-modality", "MR")
	for _, want := range []string{
		"Patient's Name                           VIVALDI^ANTONIO",
		"Accession Number                         A1",
		"Scheduled Procedure Step Sequence",
		"Scheduled Station AE Title               AA32",
		"Scheduled Procedure Step ID              SPS-A1",
		"Found 1 result(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("findscu -worklist -modality MR: output lacks %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"HAYDN", "Pre-Medication", "Query/Retrieve Level"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("findscu -worklist -modality MR: output holds %q, which was not matched or asked for:\n%s",
				unwanted, out)
		}
	}

	if out := findscu("-date", "20261119", "-station-aet", "AB45"); !strings.Contains(out, "HAYDN^FRANZ") ||
		!strings.Contains(out, "Found 1 result(s)") {
		t.Errorf("by date and station: want the CT entry alone:\n%s", out)
	}
	if out := findscu("-patient-name", "*"); !strings.Contains(out, "Found 2 result(s)") {
		t.Errorf("universal: want both entries:\n%s", out)
	}

	writeWorklistFile(t, dir, "mr2", scheduledExam(t, "A3", "MOZART^WOLFGANG", "MR", "AA33", "20261120"))
	if out := findscu("-modality", "MR"); !strings.Contains(out, "MOZART^WOLFGANG") ||
		!strings.Contains(out, "Found 2 result(s)") {
		t.Errorf("a file added while the server runs is not served:\n%s", out)
	}
	if err := os.Remove(filepath.Join(dir, "mr1.wl")); err != nil {
		t.Fatal(err)
	}
	if out := findscu("-modality", "MR"); strings.Contains(out, "VIVALDI") || !strings.Contains(out, "Found 1 result(s)") {
		t.Errorf("a removed file is still served:\n%s", out)
	}
	if out := findscu("-modality", "US"); !strings.Contains(out, "Found 0 result(s)") {
		t.Errorf("a query that matches nothing: want no results:\n%s", out)
	}

	log := serverOut.String()
	if n := strings.Count(log, "Not served: notes.txt: "); n != 1 {
		t.Errorf("the file that is not a worklist entry was reported %d times over six queries, want once:\n%s", n, log)
	}
	if image != "" {
		want := "Not served: " + image + ": no Scheduled Procedure Step Sequence (0040,0100)"
		if n := strings.Count(log, want); n != 1 {
			t.Errorf("the corpus image was reported %d times, want once:\n%s", n, log)
		}
	}
	if !strings.Contains(log, "Query from FINDSCU: 1 of 2 entries matched") {
		t.Errorf("the server did not report the first query:\n%s", log)
	}
}

// TestWlmscpRefusesOtherSOPClasses: wlmscp answers C-ECHO and Modality
// Worklist C-FIND and nothing else; a Study Root query is refused at
// negotiation rather than answered from a worklist.
func TestWlmscpRefusesOtherSOPClasses(t *testing.T) {
	binary := buildCLI(t)
	dir := t.TempDir()
	writeWorklistFile(t, dir, "mr1.wl", scheduledExam(t, "A1", "VIVALDI^ANTONIO", "MR", "AA32", "20261118"))
	address, _ := startWlmscp(t, binary, dir)

	out, err := exec.Command(binary, "echoscu", "-aec", "WLM", "-timeout", "10", address).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "C-ECHO response: Success") {
		t.Fatalf("echoscu: exit %v, output:\n%s", err, out)
	}

	out, err = exec.Command(binary, "findscu", "-aec", "WLM", "-timeout", "10", "-level", "STUDY",
		"-patient-name", "*", address).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "the peer does not support that SOP class") {
		t.Fatalf("a Study Root query was not refused at negotiation: exit %v, output:\n%s", err, out)
	}
}

// TestWlmscpNeedsADirectory: there is nothing to serve without one, and a
// path that is not a directory is said to be so before anything listens.
func TestWlmscpNeedsADirectory(t *testing.T) {
	binary := buildCLI(t)
	file := filepath.Join(t.TempDir(), "entry.wl")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, "usage: wlmscp -dir"},
		{[]string{"-dir", file}, "is not a directory"},
		{[]string{"-dir", filepath.Join(file, "missing")}, "the worklist directory"},
	} {
		out, err := exec.Command(binary, append([]string{"wlmscp"}, c.args...)...).CombinedOutput()
		if err == nil || !strings.Contains(string(out), c.want) {
			t.Errorf("wlmscp %s: exit %v, output:\n%s", strings.Join(c.args, " "), err, out)
		}
	}
}
