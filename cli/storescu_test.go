package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// writeInstance writes a minimal instance of a SOP class in a transfer syntax.
func writeInstance(t *testing.T, dir, name, class, instance, syntax string, pixels []byte) string {
	t.Helper()

	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, class))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, instance))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, "P116"))
	if pixels != nil {
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, pixels))
	}

	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(f))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID: class, MediaStorageSOPInstanceUID: instance, TransferSyntaxUID: syntax,
	})
	_ = w.AddDataElements(filewriter.ElementsFromDataset(ds))
	if err := w.Write(); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStoreSCUSendsWhatTheDefaultSetLeftOut is #116. storescu proposed the
// library's default contexts whatever it held, so these classes failed against
// an SCP that supports every one of them, with "not among the presentation
// contexts proposed".
func TestStoreSCUSendsWhatTheDefaultSetLeftOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1",
	}, dcmstore.NewHandler(store))
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes(dcmstore.SupportedSOPClasses())
	server.SetSupportedTransferSyntaxes(dcmstore.SupportedTransferSyntaxes())

	const explicitLE, implicitLE = "1.2.840.10008.1.2.1", "1.2.840.10008.1.2"
	dir := t.TempDir()
	rle := []byte{0xFE, 0xFF, 0x00, 0xE0, 0, 0, 0, 0, 0xFE, 0xFF, 0x00, 0xE0, 2, 0, 0, 0, 1, 2}
	sent := map[string]string{
		"1.2.116.1": writeInstance(t, dir, "rtdose.dcm", "1.2.840.10008.5.1.4.1.1.481.2", "1.2.116.1", explicitLE, nil),
		"1.2.116.2": writeInstance(t, dir, "rtplan.dcm", "1.2.840.10008.5.1.4.1.1.481.5", "1.2.116.2", implicitLE, nil),
		"1.2.116.3": writeInstance(t, dir, "sr.dcm", "1.2.840.10008.5.1.4.1.1.88.33", "1.2.116.3", explicitLE, nil),
		"1.2.116.4": writeInstance(t, dir, "ecg.dcm", "1.2.840.10008.5.1.4.1.1.9.1.1", "1.2.116.4", explicitLE, nil),
		// Two syntaxes of one class: each needs its own context.
		"1.2.116.5": writeInstance(t, dir, "ct.dcm", "1.2.840.10008.5.1.4.1.1.2", "1.2.116.5", explicitLE, nil),
		"1.2.116.6": writeInstance(t, dir, "ct-rle.dcm", "1.2.840.10008.5.1.4.1.1.2", "1.2.116.6",
			"1.2.840.10008.1.2.5", rle),
	}
	notDICOM := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notDICOM, []byte("not a DICOM file"), 0o600); err != nil {
		t.Fatal(err)
	}

	args := []string{"-aec", "ARCHIVE", server.Addr()}
	for _, path := range sent {
		args = append(args, path)
	}
	args = append(args, notDICOM)

	err = (&StoreSCUCommand{}).Execute(args)
	// The unreadable file is reported and counted; everything else is sent.
	if err == nil || !strings.Contains(err.Error(), "1 file(s) failed") {
		t.Errorf("Execute = %v, want exactly the unreadable file to fail", err)
	}
	for uid, path := range sent {
		inst, ok := store.Instance(uid)
		if !ok {
			t.Errorf("%s was not stored", filepath.Base(path))
			continue
		}
		if filepath.Base(path) == "ct-rle.dcm" {
			back, err := store.Load(ctx, inst)
			if err != nil {
				t.Fatal(err)
			}
			if got := back.TransferSyntaxUID(); got != "1.2.840.10008.1.2.5" {
				t.Errorf("the RLE instance was stored as %s: it went out on the wrong context", got)
			}
		}
	}
}

// TestPlanStoreBatches covers the proposal: one context per class and syntax,
// the file's own syntax first, and no association with more than 128.
func TestPlanStoreBatches(t *testing.T) {
	files := []storeFile{
		{"a", "1.2.1", "1.2.840.10008.1.2.4.70"},
		{"b", "1.2.1", "1.2.840.10008.1.2.1"},
		{"c", "1.2.1", "1.2.840.10008.1.2.4.70"},
	}
	batches := planStoreBatches(files)
	if len(batches) != 1 || len(batches[0].contexts) != 2 || len(batches[0].files) != 3 {
		t.Fatalf("got %+v, want one batch, two contexts, three files", batches)
	}
	jpeg := batches[0].contexts[0]
	if want := []string{"1.2.840.10008.1.2.4.70", "1.2.840.10008.1.2.1", "1.2.840.10008.1.2"}; strings.Join(jpeg.TransferSyntaxes, " ") != strings.Join(want, " ") {
		t.Errorf("JPEG context proposes %v, want %v", jpeg.TransferSyntaxes, want)
	}
	if explicit := batches[0].contexts[1]; len(explicit.TransferSyntaxes) != 2 {
		t.Errorf("Explicit VR context proposes %v, want it and Implicit VR, once each", explicit.TransferSyntaxes)
	}

	// 300 classes: three associations, every file in exactly one, IDs odd and
	// within 1..255.
	files = nil
	for i := 0; i < 300; i++ {
		files = append(files, storeFile{path: fmt.Sprintf("f%d", i), sopClass: fmt.Sprintf("1.2.3.%d", i), syntax: "1.2.840.10008.1.2.1"})
	}
	batches = planStoreBatches(files)
	total := 0
	for _, b := range batches {
		if len(b.contexts) > maxContexts {
			t.Errorf("a batch proposes %d contexts, over the limit of %d", len(b.contexts), maxContexts)
		}
		for _, pc := range b.contexts {
			if pc.ID%2 == 0 || pc.ID == 0 {
				t.Errorf("context ID %d is not odd and non-zero", pc.ID)
			}
		}
		total += len(b.files)
	}
	if len(batches) != 3 || total != 300 {
		t.Errorf("got %d batches carrying %d files, want 3 and 300", len(batches), total)
	}
}
