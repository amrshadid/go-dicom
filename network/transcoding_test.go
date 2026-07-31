package network

import (
	"context"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestDataSetIsEncodedInTheNegotiatedSyntax verifies a data set held in memory
// transfers correctly over any of the four uncompressed syntaxes.
//
// The conformance statement used to say "no transcoding" without qualification,
// which overstated it. The data set itself is always encoded in whatever syntax
// the presentation context negotiated — implicit or explicit VR, either byte
// order, deflated or not — so a value built in memory reaches the peer intact
// whichever was agreed. What is not re-encoded is compressed *pixel data*, since
// that needs a codec.
//
// Rows is checked as well as a text value because it is a US: byte-order
// sensitive, so it is the field that would come back as 16384 instead of 64 if
// the encoder ignored the negotiated syntax.
func TestDataSetIsEncodedInTheNegotiatedSyntax(t *testing.T) {
	for _, ts := range []string{
		ImplicitVRLittleEndianUID,
		ExplicitVRLittleEndianUID,
		ExplicitVRBigEndianUID,
		DeflatedExplicitVRLittleEndianUID,
	} {
		t.Run(ts, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			var gotName string
			var gotRows []byte
			server, err := StartServer(ctx, SCPConfig{
				AETitle: "TC_SCP", Port: 0, BindAddress: "127.0.0.1",
			}, &StorageHandler{
				OnStore: func(_ context.Context, _, _ string, ds *dataset.Dataset) uint16 {
					if e, ok := ds.Get(tag.New(0x0010, 0x0010)); ok {
						gotName = string(e.GetValue().([]byte))
					}
					if e, ok := ds.Get(tag.New(0x0028, 0x0010)); ok {
						gotRows, _ = e.GetValue().([]byte)
					}
					return StatusSuccess
				},
			})
			if err != nil {
				t.Fatalf("StartServer: %v", err)
			}
			defer server.Stop()
			server.SetSupportedTransferSyntaxes([]string{ts})

			scu := NewSCU(SCUConfig{CallingAE: "TC_SCU", CalledAE: "TC_SCP", Address: server.Addr()})
			if err := scu.Associate(ctx, []PresentationContextItem{{
				ID: 1, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ts},
			}}); err != nil {
				t.Fatalf("Associate over %s: %v", ts, err)
			}
			defer func() { _ = scu.Release(ctx) }()

			// A data set built in memory, i.e. little endian values.
			ds := dataset.NewDataset()
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(CTImageStorageUID)))
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.3.4.5\x00")))
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John")))
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.US, []byte{0x40, 0x00})) // 64

			if err := scu.Store(ctx, ds); err != nil {
				t.Fatalf("Store over %s: %v", ts, err)
			}
			if gotName != "Doe^John" {
				t.Errorf("PatientName = %q over %s", gotName, ts)
			}
			if len(gotRows) != 2 || gotRows[0] != 0x40 || gotRows[1] != 0x00 {
				t.Errorf("Rows = % X over %s, want 40 00 (64 little endian)", gotRows, ts)
			}
		})
	}
}
