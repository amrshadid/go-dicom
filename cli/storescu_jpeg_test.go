package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"path/filepath"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
	"os"
)

// The library can send an image as JPEG Baseline (#243) and storescu could
// not ask it to (#252).

const (
	secondaryCapture = "1.2.840.10008.5.1.4.1.1.7"
	jpegBaseline     = "1.2.840.10008.1.2.4.50"
	explicitLE       = "1.2.840.10008.1.2.1"
)

// writeGrayImage writes a 16 by 16 grayscale ramp, 8 or 16 bits deep, and
// returns its path and its samples.
func writeGrayImage(t *testing.T, dir, name, instance string, bits int) (string, []byte) {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID(explicitLE)
	text := func(t tag.Tag, vr dataelem.VR, v string) {
		if len(v)%2 == 1 {
			if vr == dataelem.UI {
				v += "\x00"
			} else {
				v += " "
			}
		}
		_ = ds.Add(dataelem.NewDataElement(t, vr, []byte(v)))
	}
	us := func(element uint16, v int) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(v))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b))
	}
	text(tag.New(0x0008, 0x0016), dataelem.UI, secondaryCapture)
	text(tag.New(0x0008, 0x0018), dataelem.UI, instance)
	text(tag.New(0x0010, 0x0020), dataelem.LO, "P252")
	text(tag.New(0x0020, 0x000D), dataelem.UI, "1.2.252.1")
	text(tag.New(0x0020, 0x000E), dataelem.UI, "1.2.252.2")
	text(tag.New(0x0028, 0x0004), dataelem.CS, "MONOCHROME2")
	us(0x0002, 1)
	us(0x0010, 16)
	us(0x0011, 16)
	us(0x0100, bits)
	us(0x0101, bits)
	us(0x0102, bits-1)
	us(0x0103, 0)
	px := make([]byte, 0, 256*bits/8)
	for i := 0; i < 256; i++ {
		v := (i/16 + i%16) * 8
		px = append(px, byte(v))
		if bits == 16 {
			px = append(px, 0)
		}
	}
	vr := dataelem.OB
	if bits == 16 {
		vr = dataelem.OW
	}
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), vr, px))

	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(f))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID: secondaryCapture, MediaStorageSOPInstanceUID: instance, TransferSyntaxUID: explicitLE,
	})
	_ = w.AddDataElements(filewriter.ElementsFromDataset(ds))
	if err := w.Write(); err != nil {
		t.Fatal(err)
	}
	return path, px
}

// startArchive runs a storage SCP that accepts the given transfer syntaxes.
func startArchive(t *testing.T, ctx context.Context, syntaxes []string) (*dcmstore.Store, string) {
	t.Helper()
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
	t.Cleanup(server.Stop)
	server.SetSupportedAbstractSyntaxes(dcmstore.SupportedSOPClasses())
	server.SetSupportedTransferSyntaxes(syntaxes)
	return store, server.Addr()
}

// stored loads an instance back and returns its syntax and decoded samples.
func stored(t *testing.T, ctx context.Context, store *dcmstore.Store, uid string) (string, []byte) {
	t.Helper()
	inst, ok := store.Instance(uid)
	if !ok {
		t.Fatalf("%s was not stored", uid)
	}
	ds, err := store.Load(ctx, inst)
	if err != nil {
		t.Fatal(err)
	}
	syntax := ds.TransferSyntaxUID()
	if err := ds.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	elem, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	px, _ := elem.GetValue().([]byte)
	return syntax, px
}

func TestStoreSCUSendsAsJPEGWhenAskedAndAccepted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, addr := startArchive(t, ctx, dcmstore.SupportedTransferSyntaxes())

	dir := t.TempDir()
	eight, px := writeGrayImage(t, dir, "eight.dcm", "1.2.252.8", 8)
	sixteen, deep := writeGrayImage(t, dir, "sixteen.dcm", "1.2.252.16", 16)

	if err := (&StoreSCUCommand{}).Execute([]string{"-aec", "ARCHIVE", "-jpeg-quality", "90", addr, eight, sixteen}); err != nil {
		t.Fatal(err)
	}

	syntax, got := stored(t, ctx, store, "1.2.252.8")
	if syntax != jpegBaseline {
		t.Errorf("the 8-bit image was stored as %s, want JPEG Baseline", syntax)
	}
	if len(got) != len(px) {
		t.Fatalf("%d samples came back for %d", len(got), len(px))
	}
	for i := range px {
		if d := int(got[i]) - int(px[i]); d > 8 || d < -8 {
			t.Fatalf("sample %d is %d, sent as %d: not the image at quality 90", i, got[i], px[i])
		}
	}

	// Baseline holds 8 bits, so a deeper image goes as it is.
	syntax, got = stored(t, ctx, store, "1.2.252.16")
	if syntax == jpegBaseline || !bytes.Equal(got, deep) {
		t.Errorf("the 16-bit image was stored as %s, changed: %v", syntax, !bytes.Equal(got, deep))
	}
}

func TestStoreSCUSendsAsItIsWhenThePeerRefusesJPEG(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, addr := startArchive(t, ctx, []string{explicitLE, "1.2.840.10008.1.2"})

	path, px := writeGrayImage(t, t.TempDir(), "eight.dcm", "1.2.252.9", 8)
	if err := (&StoreSCUCommand{}).Execute([]string{"-aec", "ARCHIVE", "-jpeg-quality", "90", addr, path}); err != nil {
		t.Fatal(err)
	}
	if syntax, got := stored(t, ctx, store, "1.2.252.9"); syntax == jpegBaseline || !bytes.Equal(got, px) {
		t.Errorf("stored as %s, changed: %v; want the image as it was", syntax, !bytes.Equal(got, px))
	}
}

func TestStoreSCUSendsNothingLossyUnasked(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, addr := startArchive(t, ctx, dcmstore.SupportedTransferSyntaxes())

	path, px := writeGrayImage(t, t.TempDir(), "eight.dcm", "1.2.252.10", 8)
	if err := (&StoreSCUCommand{}).Execute([]string{"-aec", "ARCHIVE", addr, path}); err != nil {
		t.Fatal(err)
	}
	if syntax, got := stored(t, ctx, store, "1.2.252.10"); syntax == jpegBaseline || !bytes.Equal(got, px) {
		t.Errorf("stored as %s, changed: %v; want the image as it was", syntax, !bytes.Equal(got, px))
	}
	if err := (&StoreSCUCommand{}).Execute([]string{"-jpeg-quality", "101", addr, path}); err == nil {
		t.Error("quality 101 was accepted")
	}
}

func TestPlanStoreBatchesProposesJPEGBesideANativeClass(t *testing.T) {
	files := []storeFile{
		{"a", "1.2.1", explicitLE},
		{"b", "1.2.1", "1.2.840.10008.1.2.5"},
	}
	batches := planStoreBatches(files, true)
	if len(batches) != 1 || len(batches[0].contexts) != 3 {
		t.Fatalf("got %+v, want one batch of three contexts", batches)
	}
	var jpegOnly int
	for _, pc := range batches[0].contexts {
		if len(pc.TransferSyntaxes) == 1 && pc.TransferSyntaxes[0] == jpegBaseline {
			jpegOnly++
		}
	}
	// A context of its own: offered beside the others, the peer could pick
	// any, and the choice would not be this command's.
	if jpegOnly != 1 {
		t.Errorf("%d contexts offer JPEG Baseline alone, want 1", jpegOnly)
	}
	if got := planStoreBatches(files, false); len(got[0].contexts) != 2 {
		t.Errorf("without the option %d contexts are proposed, want 2", len(got[0].contexts))
	}
}
