package network

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// TestEndToEndFileStoreRoundTrip exercises the complete path a real deployment
// takes: write a DICOM Part 10 file, read it back, send it over the wire with
// C-STORE, have the SCP write it to disk, then read that file and confirm every
// value survived.
//
// This is the integration guard for two bugs that made the library unusable
// with any external DICOM tool:
//   - the file meta header was written with the wrong group-0002 tags, so a
//     written file could not be read back even by this library
//   - data sets were sent as implicit VR regardless of the negotiated transfer
//     syntax, so any peer accepting explicit VR received garbage
func TestEndToEndFileStoreRoundTrip(t *testing.T) {
	const (
		sopClassUID    = CTImageStorageUID
		sopInstanceUID = "1.2.3.4.5.6.7.8.9.100"
		patientName    = "Doe^John"
		patientID      = "PID-12345"
		studyUID       = "1.2.3.4.5.6.100"
		seriesUID      = "1.2.3.4.5.6.101"
	)

	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "source.dcm")
	outDir := filepath.Join(tmp, "received")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// === 1. Write a DICOM Part 10 file ===
	writeTestFile(t, srcPath, sopClassUID, sopInstanceUID, map[tag.Tag]struct {
		vr    string
		value string
	}{
		tag.New(0x0008, 0x0016): {"UI", sopClassUID},
		tag.New(0x0008, 0x0018): {"UI", sopInstanceUID},
		tag.New(0x0008, 0x0060): {"CS", "CT"},
		tag.New(0x0010, 0x0010): {"PN", patientName},
		tag.New(0x0010, 0x0020): {"LO", patientID},
		tag.New(0x0020, 0x000D): {"UI", studyUID},
		tag.New(0x0020, 0x000E): {"UI", seriesUID},
	})

	// === 2. Read it back — the meta header must survive intact ===
	srcFile := readTestFile(t, srcPath)
	if got := srcFile.FileMetaInfo.MediaStorageSOPClassUID; got != sopClassUID {
		t.Fatalf("written file: SOP Class UID = %q, want %q", got, sopClassUID)
	}
	if got := srcFile.FileMetaInfo.MediaStorageSOPInstanceUID; got != sopInstanceUID {
		t.Fatalf("written file: SOP Instance UID = %q, want %q", got, sopInstanceUID)
	}
	if got := srcFile.FileMetaInfo.TransferSyntaxUID; got != ExplicitVRLittleEndianUID {
		t.Fatalf("written file: Transfer Syntax = %q, want %q", got, ExplicitVRLittleEndianUID)
	}
	if len(srcFile.Warnings) != 0 {
		t.Errorf("written file produced parse warnings: %v", srcFile.Warnings)
	}

	// === 3. Stand up an SCP that writes what it receives to disk ===
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var mu sync.Mutex
	var storedPaths []string

	server, err := StartServer(ctx, SCPConfig{
		AETitle:     "E2E_SCP",
		Port:        0,
		BindAddress: "127.0.0.1",
	}, &StorageHandler{
		OnStore: func(_ context.Context, sc, si string, ds *dataset.Dataset) uint16 {
			path := filepath.Join(outDir, si+".dcm")
			if err := writeReceivedDataset(path, sc, si, ds); err != nil {
				t.Errorf("SCP failed to write %s: %v", path, err)
				return StatusUnableToProcess
			}
			mu.Lock()
			storedPaths = append(storedPaths, path)
			mu.Unlock()
			return StatusSuccess
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{VerificationSOPClassUID, sopClassUID})

	// === 4. Send the file with C-STORE ===
	scu := NewSCU(SCUConfig{
		CallingAE: "E2E_SCU",
		CalledAE:  "E2E_SCP",
		Address:   server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}

	// The negotiated syntax must be explicit VR — it is proposed first — which
	// is precisely the case the old implicit-only encoder got wrong.
	pcID, ok := FindPresentationContextID(scu.Association().AcceptedContexts(), sopClassUID)
	if !ok {
		t.Fatal("no accepted presentation context for CT Image Storage")
	}
	if ts := scu.Association().TransferSyntaxFor(pcID); ts != ExplicitVRLittleEndianUID {
		t.Fatalf("negotiated transfer syntax = %q, want %q", ts, ExplicitVRLittleEndianUID)
	}

	if err := scu.Store(ctx, srcFile.GetDataset()); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := scu.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// === 5. Read the file the SCP wrote and compare ===
	mu.Lock()
	paths := append([]string(nil), storedPaths...)
	mu.Unlock()

	if len(paths) != 1 {
		t.Fatalf("SCP stored %d files, want 1", len(paths))
	}

	got := readTestFile(t, paths[0])
	if len(got.Warnings) != 0 {
		t.Errorf("received file produced parse warnings: %v", got.Warnings)
	}
	if got.FileMetaInfo.MediaStorageSOPClassUID != sopClassUID {
		t.Errorf("received: SOP Class UID = %q, want %q",
			got.FileMetaInfo.MediaStorageSOPClassUID, sopClassUID)
	}
	if got.FileMetaInfo.MediaStorageSOPInstanceUID != sopInstanceUID {
		t.Errorf("received: SOP Instance UID = %q, want %q",
			got.FileMetaInfo.MediaStorageSOPInstanceUID, sopInstanceUID)
	}

	gotDS := got.GetDataset()
	want := map[tag.Tag]string{
		tag.New(0x0008, 0x0016): sopClassUID,
		tag.New(0x0008, 0x0018): sopInstanceUID,
		tag.New(0x0008, 0x0060): "CT",
		tag.New(0x0010, 0x0010): patientName,
		tag.New(0x0010, 0x0020): patientID,
		tag.New(0x0020, 0x000D): studyUID,
		tag.New(0x0020, 0x000E): seriesUID,
	}
	for tg, expect := range want {
		elem, ok := gotDS.Get(tg)
		if !ok {
			t.Errorf("received file is missing %s", tg)
			continue
		}
		if actual := trimPadding(elem.GetValue().([]byte)); actual != expect {
			t.Errorf("received %s = %q, want %q", tg, actual, expect)
		}
	}
}

// TestEndToEndAllValuesAreEvenLength verifies that every element in a file
// produced by this library has an even length, as DICOM requires. Odd lengths
// are what conforming implementations reject.
func TestEndToEndAllValuesAreEvenLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odd.dcm")

	// Deliberately odd-length values throughout.
	writeTestFile(t, path, CTImageStorageUID, "1.2.3.4.5.6.7.8.9", map[tag.Tag]struct {
		vr    string
		value string
	}{
		tag.New(0x0008, 0x0016): {"UI", CTImageStorageUID},   // 25
		tag.New(0x0008, 0x0018): {"UI", "1.2.3.4.5.6.7.8.9"}, // 17
		tag.New(0x0010, 0x0010): {"PN", "Doe^Jon"},           // 7
		tag.New(0x0010, 0x0020): {"LO", "ODD"},               // 3
	})

	df := readTestFile(t, path)
	if len(df.Warnings) != 0 {
		t.Errorf("parse warnings: %v", df.Warnings)
	}

	for _, elem := range df.DataElements {
		if elem.Length%2 != 0 {
			t.Errorf("element %s has odd length %d", elem.Tag, elem.Length)
		}
	}

	// A misaligned stream would drop or mangle later elements.
	if len(df.DataElements) != 4 {
		t.Errorf("parsed %d elements, want 4 — stream is likely misaligned", len(df.DataElements))
	}
}

// writeTestFile writes a DICOM Part 10 file with the given meta and elements.
func writeTestFile(t *testing.T, path, sopClassUID, sopInstanceUID string, elements map[tag.Tag]struct {
	vr    string
	value string
}) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create %s: %v", path, err)
	}
	defer f.Close()

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(f))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    sopClassUID,
		MediaStorageSOPInstanceUID: sopInstanceUID,
		TransferSyntaxUID:          ExplicitVRLittleEndianUID,
		ImplementationClassUID:     DefaultImplementationClassUID,
		ImplementationVersionName:  DefaultImplementationVersionName,
	})

	// Write in ascending tag order, as DICOM requires.
	for _, tg := range sortedTags(elements) {
		e := elements[tg]
		if err := w.AddDataElement(&filewriter.DataElement{
			Tag:    tg,
			VR:     e.vr,
			Value:  []byte(e.value),
			Length: uint32(len(e.value)),
		}); err != nil {
			t.Fatalf("AddDataElement %s: %v", tg, err)
		}
	}

	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// writeReceivedDataset writes a dataset received over the network to disk.
func writeReceivedDataset(path, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(f))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    sopClassUID,
		MediaStorageSOPInstanceUID: sopInstanceUID,
		TransferSyntaxUID:          ExplicitVRLittleEndianUID,
		ImplementationClassUID:     DefaultImplementationClassUID,
		ImplementationVersionName:  DefaultImplementationVersionName,
	})

	for _, elem := range ds.GetAll() {
		tg, ok := elem.GetTag().(tag.Tag)
		if !ok {
			continue
		}
		data, ok := elem.GetValue().([]byte)
		if !ok {
			continue
		}
		if err := w.AddDataElement(&filewriter.DataElement{
			Tag:    tg,
			VR:     string(elem.GetVR()),
			Value:  data,
			Length: uint32(len(data)),
		}); err != nil {
			return err
		}
	}

	if err := w.Write(); err != nil {
		return err
	}
	return w.Close()
}

// readTestFile parses a DICOM file from disk.
func readTestFile(t *testing.T, path string) *filereader.DICOMFile {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open %s: %v", path, err)
	}
	defer f.Close()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatalf("ReadDICOMFile %s: %v", path, err)
	}
	return df
}

// sortedTags returns the map's keys in ascending tag order.
func sortedTags(m map[tag.Tag]struct {
	vr    string
	value string
}) []tag.Tag {
	out := make([]tag.Tag, 0, len(m))
	for tg := range m {
		out = append(out, tg)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
