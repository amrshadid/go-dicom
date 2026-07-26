package network

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
)

// The decoders in this package parse binary input supplied by an
// unauthenticated peer. Four denial-of-service defects were found in them by
// hand-reading code, which is not a repeatable process. These targets let the
// fuzzer look instead.
//
// A decoder may reject anything; what it must never do is panic, hang, or
// allocate without bound. Run locally with:
//
//	go test ./network/ -run=Fuzz -fuzz=FuzzDecodePDU -fuzztime=60s

// FuzzDecodePDU exercises PDU decoding, the first thing that touches bytes off
// the wire.
func FuzzDecodePDU(f *testing.F) {
	// Seed with one well-formed PDU of each type, so the fuzzer starts from
	// structurally valid input rather than having to discover the framing.
	for _, pdu := range []PDU{
		&ReleaseRQ{},
		&ReleaseRP{},
		&AbortPDU{Source: AbortSourceServiceUser, Reason: 0},
		&AssociateRJ{Result: RJResultRejectedPermanent, Source: RJSourceServiceUser, Reason: 1},
	} {
		if encoded, err := pdu.Encode(); err == nil {
			f.Add(encoded)
		}
	}

	rq := &AssociateRQ{
		ProtocolVersion:       ProtocolVersion,
		CalledAE:              "TARGET",
		CallingAE:             "SOURCE",
		ApplicationContextUID: DefaultApplicationContextUID,
		PresentationContexts: []PresentationContextItem{
			{ID: 1, AbstractSyntax: VerificationSOPClassUID,
				TransferSyntaxes: DefaultTransferSyntaxes()},
		},
		UserInformation: UserInformationItem{MaxPDULength: DefaultMaxPDUSize},
	}
	if encoded, err := rq.Encode(); err == nil {
		f.Add(encoded)
	}

	// A P-DATA-TF carrying a command.
	var pdata bytes.Buffer
	_ = binary.Write(&pdata, binary.BigEndian, uint32(6))
	pdata.Write([]byte{0x01, 0x03, 0xDE, 0xAD, 0xBE, 0xEF})
	f.Add(wrapPDU(PDUTypeDataTF, pdata.Bytes()))

	// Degenerate inputs: empty, header-only, and a truncated length field.
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00})
	f.Add([]byte{0x04, 0x00, 0xFF, 0xFF, 0xFF, 0xFF})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Any error is acceptable; a panic or hang is not.
		pdu, err := DecodePDU(bytes.NewReader(data))
		if err != nil {
			return
		}
		if pdu == nil {
			t.Fatal("DecodePDU returned nil PDU with nil error")
		}
		// Re-encoding a decoded PDU must not panic either.
		_, _ = pdu.Encode()
	})
}

// FuzzDecodeDataset exercises data set decoding across every transfer syntax,
// including the deflated one that carries its own decompression risk.
func FuzzDecodeDataset(f *testing.F) {
	syntaxes := []string{
		ImplicitVRLittleEndianUID,
		ExplicitVRLittleEndianUID,
		ExplicitVRBigEndianUID,
		DeflatedExplicitVRLittleEndianUID,
	}

	// Seed with a real encoded data set per syntax.
	ds := buildCodecTestDataset()
	for i, ts := range syntaxes {
		if encoded, err := EncodeDataset(ds, ts); err == nil {
			f.Add(encoded, i)
		}
	}

	// A sequence, which is the most structurally involved thing the decoder
	// handles and where nesting bugs would surface.
	f.Add([]byte{
		0x40, 0x00, 0x30, 0xA7, // (0040,A730)
		'S', 'Q', 0x00, 0x00,
		0x10, 0x00, 0x00, 0x00, // 16 bytes of items
		0xFE, 0xFF, 0x00, 0xE0, // item
		0x08, 0x00, 0x00, 0x00, // 8 bytes
		0x08, 0x00, 0x00, 0x01, 'S', 'H', 0x00, 0x00,
	}, 1)

	f.Add([]byte{}, 0)
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF}, 1)

	f.Fuzz(func(t *testing.T, data []byte, syntaxIndex int) {
		// Convert through uint before the modulo: negating math.MinInt64
		// overflows back to itself, so a sign flip would leave the index
		// negative and index out of range.
		ts := syntaxes[uint(syntaxIndex)%uint(len(syntaxes))]

		decoded, err := DecodeDataset(data, ts)
		if err != nil {
			return
		}
		if decoded == nil {
			t.Fatal("DecodeDataset returned nil dataset with nil error")
		}
		// Re-encoding what was decoded must not panic.
		_, _ = EncodeDataset(decoded, ts)
	})
}

// FuzzDecodeCommandDataset exercises DIMSE command parsing, which runs on every
// message before any handler sees it.
func FuzzDecodeCommandDataset(f *testing.F) {
	for _, ds := range []*dataset.Dataset{
		BuildCEchoRQ(1),
		BuildCEchoRSP(1, StatusSuccess),
		BuildCStoreRQ(1, CTImageStorageUID, "1.2.3", PriorityMedium),
		BuildCFindRQ(1, StudyRootQueryRetrieveFind, PriorityMedium),
		BuildCMoveRQ(1, StudyRootQueryRetrieveMove, "DEST", PriorityMedium),
		BuildCGetRSP(1, StudyRootQueryRetrieveGet, StatusPending, 1, 0, 0, 0),
	} {
		if encoded, err := EncodeCommandDataset(ds); err == nil {
			f.Add(encoded)
		}
	}

	f.Add([]byte{})
	// A declared length far larger than the buffer.
	f.Add([]byte{0x00, 0x00, 0x00, 0x01, 0xFF, 0xFF, 0xFF, 0x7F, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		ds, err := DecodeCommandDataset(data)
		if err != nil {
			return
		}
		if ds == nil {
			t.Fatal("DecodeCommandDataset returned nil dataset with nil error")
		}
		// The accessors run on every received message, so they must tolerate
		// whatever the decoder produced.
		_, _, _, _ = ParseCommandDataset(ds)
		_ = HasDataSet(ds)
		_, _ = GetAffectedSOPClassUID(ds)
		_, _ = GetAffectedSOPInstanceUID(ds)
	})
}
