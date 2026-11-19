package network_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

var (
	nestedPrivate = tag.New(0x0001, 0x0001)
	unSequence    = tag.New(0x4453, 0x100C)
)

// corpusDataSetBytes returns a corpus file's data set as it is encoded in the
// file, after the File Meta Information: the bytes a peer that sends the file
// as it is puts on the wire, undefined lengths and all.
func corpusDataSetBytes(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(corpusDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	// (0002,0000) is the first element after the preamble and "DICM": an
	// Explicit VR UL whose 4-byte value starts at offset 140.
	if len(raw) < 144 || string(raw[128:132]) != "DICM" {
		t.Fatalf("%s has no File Meta Information group length where expected", name)
	}
	return raw[144+int(binary.LittleEndian.Uint32(raw[140:144])):]
}

// sequenceItem returns item i of the sequence under seqTag in ds, failing when
// the element is not a sequence: the check that the reader learned it holds
// items rather than taking it for an opaque value.
func sequenceItem(t *testing.T, ds *dataset.Dataset, seqTag tag.Tag, i int) *dataset.Dataset {
	t.Helper()
	seq, err := ds.GetSequence(seqTag)
	if err != nil {
		t.Fatalf("%s is not a sequence: %v", seqTag, err)
	}
	item, err := seq.Get(i)
	if err != nil {
		t.Fatalf("%s has no item %d: %v", seqTag, i, err)
	}
	return item.(*dataset.Dataset)
}

// checkNestedPrivate checks a data set holds nested_priv_SQ.dcm's private
// sequence inside a private sequence, with both of its values.
func checkNestedPrivate(t *testing.T, ds *dataset.Dataset) {
	t.Helper()
	outer := sequenceItem(t, ds, nestedPrivate, 0)
	inner := sequenceItem(t, outer, nestedPrivate, 0)
	value, ok := inner.Get(nestedPrivate)
	if !ok {
		t.Fatal("the value inside the nested private sequence is missing")
	}
	if v, _ := value.GetValue().([]byte); string(v) != "Double Nested SQ" {
		t.Errorf("value inside the nested private sequence = %q, want %q", v, "Double Nested SQ")
	}
	after, ok := outer.Get(tag.New(0x0001, 0x0002))
	if !ok {
		t.Fatal("(0001,0002), after the nested sequence, is missing")
	}
	if v, _ := after.GetValue().([]byte); !bytes.HasPrefix(v, []byte("Nested SQ")) {
		t.Errorf("(0001,0002) = %q, want %q", v, "Nested SQ")
	}
}

// A private sequence holding a private sequence, as nested_priv_SQ.dcm has it,
// is sent in Implicit VR with undefined length and a Sequence Delimitation Item
// for each. Nothing else tells the peer it is a sequence: Implicit VR has no VR
// field and a private tag has no dictionary entry (PS3.5 6.2.2 Note 5). Sent
// with its byte count, dcmtk's storescp and pynetdicom's stored it as 66
// opaque bytes. A standard sequence keeps its byte count, and Explicit VR,
// which says SQ, writes no delimiter.
func TestPrivateSequenceIsSentWithUndefinedLengthInImplicitVR(t *testing.T) {
	ds := corpusFile(t, "nested_priv_SQ.dcm")
	referenced := tag.New(0x0008, 0x1140) // Referenced Image Sequence
	if err := ds.AddSequence(referenced, sequenceOf(t, tag.New(0x0008, 0x1150), "1.2.3")); err != nil {
		t.Fatal(err)
	}

	encoded, err := network.EncodeDataset(ds, network.ImplicitVRLittleEndianUID)
	if err != nil {
		t.Fatal(err)
	}
	header := nestedPrivate.ToBytes(true)
	first := bytes.Index(encoded, header)
	second := bytes.Index(encoded[first+8:], header) + first + 8
	if first < 0 || second < first+8 {
		t.Fatalf("the two private sequence headers are not in the encoding: % x", encoded)
	}
	for _, at := range []int{first, second} {
		if length := binary.LittleEndian.Uint32(encoded[at+4:]); length != 0xFFFFFFFF {
			t.Errorf("the private sequence at offset %d has length %#x, want undefined", at, length)
		}
	}
	delimiter := []byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0}
	if n := bytes.Count(encoded, delimiter); n != 2 {
		t.Errorf("%d Sequence Delimitation Items sent, want 2", n)
	}
	at := bytes.Index(encoded, referenced.ToBytes(true))
	if length := binary.LittleEndian.Uint32(encoded[at+4:]); length == 0xFFFFFFFF {
		t.Error("the standard Referenced Image Sequence lost its byte count")
	}

	decoded, err := network.DecodeDataset(encoded, network.ImplicitVRLittleEndianUID)
	if err != nil {
		t.Fatalf("decoding what was sent: %v", err)
	}
	checkNestedPrivate(t, decoded)

	explicit, err := network.EncodeDataset(ds, network.ExplicitVRLittleEndianUID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(explicit, delimiter) {
		t.Error("a Sequence Delimitation Item was sent in Explicit VR, where SQ says it all")
	}
}

// sequenceOf builds a one-item sequence holding one UI value.
func sequenceOf(t *testing.T, valueTag tag.Tag, value string) *sequence.Sequence {
	t.Helper()
	item := dataset.NewDataset()
	if err := item.Add(dataelem.NewDataElement(valueTag, dataelem.UI, []byte(value))); err != nil {
		t.Fatal(err)
	}
	seq := sequence.New()
	if err := seq.Append(item); err != nil {
		t.Fatal(err)
	}
	return seq
}

// nested_priv_SQ.dcm's data set as the file encodes it, which is how dcmtk,
// pynetdicom and modalities send a private sequence: undefined length, items
// of undefined length. It used to be read as encapsulated pixel data and
// refused ("a pixel data fragment declares 4294967295 bytes"), and an
// undefined-length element inside an item silently ended the item.
func TestUndefinedLengthPrivateSequenceIsReceivedAsASequence(t *testing.T) {
	ds, err := network.DecodeDataset(corpusDataSetBytes(t, "nested_priv_SQ.dcm"), network.ImplicitVRLittleEndianUID)
	if err != nil {
		t.Fatalf("decoding nested_priv_SQ.dcm's data set: %v", err)
	}
	checkNestedPrivate(t, ds)
	if _, ok := ds.Get(tag.New(0x7FE0, 0x0010)); !ok {
		t.Error("Pixel Data, after the private sequence, is missing")
	}
}

// UN_sequence.dcm holds (4453,100C) as UN with undefined length in Explicit
// VR, with standard sequences of undefined length inside it. PS3.5 6.2.2 has
// such an element read as a sequence whose value is Implicit VR Little
// Endian. It used to be read as encapsulated pixel data and refused at its
// first item.
func TestUndefinedLengthUNIsReceivedAsASequence(t *testing.T) {
	ds, err := network.DecodeDataset(corpusDataSetBytes(t, "UN_sequence.dcm"), network.ExplicitVRLittleEndianUID)
	if err != nil {
		t.Fatalf("decoding UN_sequence.dcm's data set: %v", err)
	}
	series := sequenceItem(t, sequenceItem(t, ds, unSequence, 0), tag.New(0x0008, 0x1115), 0)
	instance := sequenceItem(t, series, tag.New(0x0008, 0x1199), 0)
	uid, ok := instance.Get(tag.New(0x0008, 0x1155))
	if !ok {
		t.Fatal("Referenced SOP Instance UID deep inside the UN sequence is missing")
	}
	if v, _ := uid.GetValue().([]byte); !bytes.HasPrefix(v, []byte("1.2.840.113619.2.327.3.185221411.476.1398588726.278.80")) {
		t.Errorf("Referenced SOP Instance UID = %q", v)
	}
}

// A C-STORE in Implicit VR of an instance with private sequences reaches a
// handler that is given the decoded data set (StorageHandler, as the
// storescp command uses) with both private sequences as sequences. It used to
// be refused with 0110 on receipt, or, from this library's own sender, stored
// with the sequence as an opaque value.
func TestPrivateSequenceSurvivesAStoreInImplicitVR(t *testing.T) {
	for _, name := range []string{"nested_priv_SQ.dcm", "UN_sequence.dcm"} {
		t.Run(name, func(t *testing.T) {
			ds := corpusFile(t, name)
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(network.SecondaryCaptureImageStorageUID)))
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.826.0.1.3680043.8.498.424")))
			ds.SetTransferSyntaxUID(network.ImplicitVRLittleEndianUID)

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			received := make(chan *dataset.Dataset, 1)
			server, err := network.StartServer(ctx, network.SCPConfig{
				AETitle: "PRIVSQ", Port: 0, BindAddress: "127.0.0.1",
			}, &network.StorageHandler{
				OnStore: func(_ context.Context, _, _ string, got *dataset.Dataset) uint16 {
					received <- got
					return network.StatusSuccess
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Stop()

			scu := network.NewSCU(network.SCUConfig{CallingAE: "SCU", CalledAE: "PRIVSQ", Address: server.Addr()})
			if err := scu.Associate(ctx, []network.PresentationContextItem{{ID: 1,
				AbstractSyntax:   network.SecondaryCaptureImageStorageUID,
				TransferSyntaxes: []string{network.ImplicitVRLittleEndianUID}}}); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = scu.Release(ctx) }()
			if err := scu.Store(ctx, ds); err != nil {
				t.Fatalf("C-STORE: %v", err)
			}

			got := <-received
			if name == "nested_priv_SQ.dcm" {
				checkNestedPrivate(t, got)
				return
			}
			series := sequenceItem(t, sequenceItem(t, got, unSequence, 0), tag.New(0x0008, 0x1115), 0)
			if _, ok := sequenceItem(t, series, tag.New(0x0008, 0x1199), 0).Get(tag.New(0x0008, 0x1155)); !ok {
				t.Error("Referenced SOP Instance UID deep inside the private sequence is missing")
			}
		})
	}
}
