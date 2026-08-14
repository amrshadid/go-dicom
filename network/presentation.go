package network

import (
	"fmt"
	"strings"
)

// Common DICOM SOP Class UIDs used in networking.
const (
	VerificationSOPClassUID = "1.2.840.10008.1.1"

	// Storage SOP Classes
	CTImageStorageUID               = "1.2.840.10008.5.1.4.1.1.2"
	EnhancedCTImageStorageUID       = "1.2.840.10008.5.1.4.1.1.2.1"
	MRImageStorageUID               = "1.2.840.10008.5.1.4.1.1.4"
	EnhancedMRImageStorageUID       = "1.2.840.10008.5.1.4.1.1.4.1"
	USImageStorageUID               = "1.2.840.10008.5.1.4.1.1.6.1"
	SecondaryCaptureImageStorageUID = "1.2.840.10008.5.1.4.1.1.7"
	XRayAngiographicImageStorageUID = "1.2.840.10008.5.1.4.1.1.12.1"
	DigitalXRayImageStorageUID      = "1.2.840.10008.5.1.4.1.1.1.1"
	CRImageStorageUID               = "1.2.840.10008.5.1.4.1.1.1"

	// Query/Retrieve SOP Classes
	PatientRootQueryRetrieveFind = "1.2.840.10008.5.1.4.1.2.1.1"
	PatientRootQueryRetrieveMove = "1.2.840.10008.5.1.4.1.2.1.2"
	PatientRootQueryRetrieveGet  = "1.2.840.10008.5.1.4.1.2.1.3"
	StudyRootQueryRetrieveFind   = "1.2.840.10008.5.1.4.1.2.2.1"
	StudyRootQueryRetrieveMove   = "1.2.840.10008.5.1.4.1.2.2.2"
	StudyRootQueryRetrieveGet    = "1.2.840.10008.5.1.4.1.2.2.3"

	// Patient/Study Only Root is retired in the current standard but still
	// offered by some archives, so it is proposed after the two current models.
	PatientStudyOnlyQueryRetrieveFind = "1.2.840.10008.5.1.4.1.2.3.1"
	PatientStudyOnlyQueryRetrieveMove = "1.2.840.10008.5.1.4.1.2.3.2"
	PatientStudyOnlyQueryRetrieveGet  = "1.2.840.10008.5.1.4.1.2.3.3"
)

// DICOM Transfer Syntax UIDs — complete set matching pynetdicom.
const (
	// Uncompressed
	ImplicitVRLittleEndianUID         = "1.2.840.10008.1.2"
	ExplicitVRLittleEndianUID         = "1.2.840.10008.1.2.1"
	DeflatedExplicitVRLittleEndianUID = "1.2.840.10008.1.2.1.99"
	ExplicitVRBigEndianUID            = "1.2.840.10008.1.2.2"

	// JPEG
	JPEGBaselineUID = "1.2.840.10008.1.2.4.50"
	JPEGExtendedUID = "1.2.840.10008.1.2.4.51"
	// The two lossless syntaxes were named the wrong way round: .57 is Process
	// 14 with any selection value, and .70 is the one fixed to selection value
	// 1. A caller reaching for JPEGLosslessSV1UID got the syntax that is not
	// SV1, which is the sort of mistake a name is supposed to prevent.
	JPEGLosslessProcess14UID = "1.2.840.10008.1.2.4.57"
	JPEGLosslessSV1UID       = "1.2.840.10008.1.2.4.70"

	// Deprecated: use JPEGLosslessSV1UID, which names the same syntax
	// accurately. Retained so existing callers keep compiling.
	JPEGLosslessUID = JPEGLosslessSV1UID

	// JPEG-LS
	JPEGLSLosslessUID     = "1.2.840.10008.1.2.4.80"
	JPEGLSNearLosslessUID = "1.2.840.10008.1.2.4.81"

	// JPEG 2000
	JPEG2000LosslessUID                    = "1.2.840.10008.1.2.4.90"
	JPEG2000UID                            = "1.2.840.10008.1.2.4.91"
	JPEG2000Part2MultiComponentLosslessUID = "1.2.840.10008.1.2.4.92"
	JPEG2000Part2MultiComponentUID         = "1.2.840.10008.1.2.4.93"

	// JPIP
	JPIPReferencedUID        = "1.2.840.10008.1.2.4.94"
	JPIPReferencedDeflateUID = "1.2.840.10008.1.2.4.95"

	// MPEG2
	MPEG2MainProfileUID             = "1.2.840.10008.1.2.4.100"
	MPEG2MainProfileFragmentUID     = "1.2.840.10008.1.2.4.100.1"
	MPEG2MainProfileHighUID         = "1.2.840.10008.1.2.4.101"
	MPEG2MainProfileHighFragmentUID = "1.2.840.10008.1.2.4.101.1"

	// MPEG-4 AVC/H.264
	MPEG4AVCH264HighProfileUID           = "1.2.840.10008.1.2.4.102"
	MPEG4AVCH264HighProfileFragmentUID   = "1.2.840.10008.1.2.4.102.1"
	MPEG4AVCH264BDCompatibleUID          = "1.2.840.10008.1.2.4.103"
	MPEG4AVCH264BDCompatibleFragmentUID  = "1.2.840.10008.1.2.4.103.1"
	MPEG4AVCH264HighProfile2DUID         = "1.2.840.10008.1.2.4.104"
	MPEG4AVCH264HighProfile2DFragmentUID = "1.2.840.10008.1.2.4.104.1"
	MPEG4AVCH264HighProfile3DUID         = "1.2.840.10008.1.2.4.105"
	MPEG4AVCH264HighProfile3DFragmentUID = "1.2.840.10008.1.2.4.105.1"
	MPEG4AVCH264StereoHighProfileUID     = "1.2.840.10008.1.2.4.106"
	MPEG4AVCH264StereoHighFragmentUID    = "1.2.840.10008.1.2.4.106.1"

	// HEVC/H.265
	HEVCH265MainProfileUID   = "1.2.840.10008.1.2.4.107"
	HEVCH265Main10ProfileUID = "1.2.840.10008.1.2.4.108"

	// JPEG XL
	JPEGXLLosslessUID          = "1.2.840.10008.1.2.4.110"
	JPEGXLJPEGRecompressionUID = "1.2.840.10008.1.2.4.111"
	JPEGXLUID                  = "1.2.840.10008.1.2.4.112"

	// High-Throughput JPEG 2000
	HTJ2KLosslessUID              = "1.2.840.10008.1.2.4.201"
	HTJ2KLosslessRPCLUID          = "1.2.840.10008.1.2.4.202"
	HTJ2KUID                      = "1.2.840.10008.1.2.4.203"
	JPIPHTJ2KReferencedUID        = "1.2.840.10008.1.2.4.204"
	JPIPHTJ2KReferencedDeflateUID = "1.2.840.10008.1.2.4.205"

	// RLE
	RLELosslessUID = "1.2.840.10008.1.2.5"

	// SMPTE ST 2110
	SMPTEST2110UncompressedProgressiveUID = "1.2.840.10008.1.2.7.1"
	SMPTEST2110UncompressedInterlacedUID  = "1.2.840.10008.1.2.7.2"
	SMPTEST2110PCMDigitalAudioUID         = "1.2.840.10008.1.2.7.3"
)

// PresentationContext represents a negotiated presentation context
// pairing an abstract syntax (SOP Class) with a transfer syntax.
type PresentationContext struct {
	ID             byte
	AbstractSyntax string
	TransferSyntax string
	Result         byte

	// Proposed records the transfer syntaxes offered for this context, which the
	// A-ASSOCIATE-AC does not echo back. Without it a refusal can say the peer
	// supported none of them but not which were tried, and that is the half the
	// caller needs to widen the proposal.
	Proposed []string
}

// IsAccepted returns true if this presentation context was accepted.
func (pc *PresentationContext) IsAccepted() bool {
	return pc.Result == PCResultAcceptance
}

// DefaultTransferSyntaxes returns the default set of transfer syntaxes to propose.
// DefaultTransferSyntaxes returns the uncompressed transfer syntaxes proposed
// and accepted unless the caller asks for more.
//
// All four are fully supported: the data set codec encodes and decodes each of
// them, including the byte swap for big endian and the deflate stream. Only two
// were listed, so a peer offering deflated or big endian data had no context to
// send it on even though this library reads both.
//
// Compressed syntaxes are not here, matching pynetdicom, whose default is these
// same four with the rest behind ALL_TRANSFER_SYNTAXES. Use AllTransferSyntaxes
// to accept compressed pixel data, which an archive or router wants: it stores
// and forwards the bytes without needing to decode them.
//
// This default is deliberately conservative because it applies to every caller,
// including an SCU that will try to decode what it asked for — and for that case a
// compressed syntax with no decoder moves the failure from association time, where
// the error names the cause, to pixel access, where it does not.
//
// A server whose purpose is to store is the other case, and there the answer is
// the opposite: accept everything, since keeping an instance does not require
// decoding it. Both servers in this distribution do — see the storescp and qrscp
// commands, and dcmstore.SupportedTransferSyntaxes.
func DefaultTransferSyntaxes() []string {
	return []string{
		ExplicitVRLittleEndianUID,
		ImplicitVRLittleEndianUID,
		DeflatedExplicitVRLittleEndianUID,
		ExplicitVRBigEndianUID,
	}
}

// DefaultVerificationContexts returns presentation contexts for C-ECHO.
func DefaultVerificationContexts() []PresentationContextItem {
	return []PresentationContextItem{
		{
			ID:               1,
			AbstractSyntax:   VerificationSOPClassUID,
			TransferSyntaxes: DefaultTransferSyntaxes(),
		},
	}
}

// DefaultStorageContexts returns presentation contexts for common storage SOP classes.
func DefaultStorageContexts() []PresentationContextItem {
	storageClasses := []string{
		CTImageStorageUID,
		EnhancedCTImageStorageUID,
		MRImageStorageUID,
		EnhancedMRImageStorageUID,
		USImageStorageUID,
		SecondaryCaptureImageStorageUID,
		CRImageStorageUID,
		DigitalXRayImageStorageUID,
		XRayAngiographicImageStorageUID,
	}

	ts := DefaultTransferSyntaxes()
	contexts := make([]PresentationContextItem, len(storageClasses))
	for i, sc := range storageClasses {
		contexts[i] = PresentationContextItem{
			ID:               byte(2*i + 1), // Odd IDs: 1, 3, 5, ...
			AbstractSyntax:   sc,
			TransferSyntaxes: ts,
		}
	}
	return contexts
}

// DefaultQueryRetrieveContexts returns presentation contexts for query/retrieve operations.
func DefaultQueryRetrieveContexts() []PresentationContextItem {
	qrClasses := []string{
		PatientRootQueryRetrieveFind,
		PatientRootQueryRetrieveMove,
		PatientRootQueryRetrieveGet,
		StudyRootQueryRetrieveFind,
		StudyRootQueryRetrieveMove,
		StudyRootQueryRetrieveGet,
		PatientStudyOnlyQueryRetrieveFind,
		PatientStudyOnlyQueryRetrieveMove,
		PatientStudyOnlyQueryRetrieveGet,
	}

	ts := DefaultTransferSyntaxes()
	contexts := make([]PresentationContextItem, len(qrClasses))
	for i, qr := range qrClasses {
		contexts[i] = PresentationContextItem{
			ID:               byte(2*i + 1),
			AbstractSyntax:   qr,
			TransferSyntaxes: ts,
		}
	}
	return contexts
}

// NegotiatePresentationContexts selects transfer syntaxes for requested presentation contexts
// based on what the SCP supports. Returns the result items for the A-ASSOCIATE-AC PDU.
func NegotiatePresentationContexts(
	requested []PresentationContextItem,
	supportedAbstractSyntaxes map[string]bool,
	supportedTransferSyntaxes map[string]bool,
) []PresentationContextResultItem {
	results := make([]PresentationContextResultItem, 0, len(requested))

	for _, req := range requested {
		result := PresentationContextResultItem{
			ID: req.ID,
		}

		// A rejected result must still carry a Transfer Syntax sub-item (PS3.8 9.3.3.2),
		// so fall back to a known-valid UID when the peer proposed none. A peer is free
		// to send a presentation context with zero transfer syntax sub-items, so this
		// must never index into an empty slice.
		rejectionTS := ImplicitVRLittleEndianUID
		if len(req.TransferSyntaxes) > 0 {
			rejectionTS = req.TransferSyntaxes[0]
		}

		// Check abstract syntax support
		if !supportedAbstractSyntaxes[req.AbstractSyntax] {
			result.Result = PCResultAbstractSyntaxNotSupported
			result.TransferSyntax = rejectionTS
			results = append(results, result)
			continue
		}

		// Find a supported transfer syntax (prefer first match)
		found := false
		for _, ts := range req.TransferSyntaxes {
			if supportedTransferSyntaxes[ts] {
				result.Result = PCResultAcceptance
				result.TransferSyntax = ts
				found = true
				break
			}
		}

		if !found {
			result.Result = PCResultTransferSyntaxNotSupported
			result.TransferSyntax = rejectionTS
		}

		results = append(results, result)
	}

	return results
}

// reportRefusedContexts logs each presentation context an SCP is about to refuse,
// naming what the peer asked for.
//
// An association whose contexts are all refused still succeeds at the protocol
// level, and the requestor then fails on its first operation. From this side that
// looked like nothing at all had happened, so an operator had no way to see that
// a modality was proposing JPEG-LS to a server offering only the uncompressed
// syntaxes — the commonest cause, and one fixed by passing AllTransferSyntaxes to
// SetSupportedTransferSyntaxes.
func reportRefusedContexts(rq *AssociateRQ, results []PresentationContextResultItem) {
	proposedByID := make(map[byte]PresentationContextItem, len(rq.PresentationContexts))
	for _, pc := range rq.PresentationContexts {
		proposedByID[pc.ID] = pc
	}

	accepted := 0
	for _, res := range results {
		if res.Result == PCResultAcceptance {
			accepted++
			continue
		}

		proposed := proposedByID[res.ID]
		switch res.Result {
		case PCResultTransferSyntaxNotSupported:
			DefaultLogger.Warn("refused presentation context %d for %s from %s: "+
				"none of the transfer syntaxes it proposed (%s) is supported; "+
				"pass AllTransferSyntaxes() to SetSupportedTransferSyntaxes to accept "+
				"compressed pixel data",
				res.ID, proposed.AbstractSyntax, rq.CallingAE,
				strings.Join(proposed.TransferSyntaxes, ", "))
		case PCResultAbstractSyntaxNotSupported:
			DefaultLogger.Warn("refused presentation context %d from %s: "+
				"SOP class %s is not supported; add it with SetSupportedAbstractSyntaxes",
				res.ID, rq.CallingAE, proposed.AbstractSyntax)
		default:
			DefaultLogger.Warn("refused presentation context %d for %s from %s with result %d",
				res.ID, proposed.AbstractSyntax, rq.CallingAE, res.Result)
		}
	}

	// Every context refused is the case worth stating plainly: the association is
	// established and useless, and the requestor will not learn why until it tries
	// something.
	if accepted == 0 && len(results) > 0 {
		DefaultLogger.Error("accepted the association from %s with no usable presentation "+
			"context: all %d were refused, so every operation on it will fail",
			rq.CallingAE, len(results))
	}
}

// BuildAcceptedContextMap creates a map from presentation context ID to accepted context
// from the A-ASSOCIATE-AC response.
func BuildAcceptedContextMap(
	requested []PresentationContextItem,
	results []PresentationContextResultItem,
) map[byte]*PresentationContext {
	accepted, _ := BuildContextMaps(requested, results)
	return accepted
}

// BuildContextMaps splits the A-ASSOCIATE-AC results into what the peer accepted
// and what it refused, both keyed by presentation context ID.
//
// The refusals matter as much as the acceptances. A peer answers each proposed
// context with a reason — abstract syntax not supported, transfer syntax not
// supported, user rejection — and discarding them leaves an operation able to
// report only that no context was accepted, not why. Which of those it is
// decides what the caller should do: propose a different SOP class, or propose
// more transfer syntaxes.
func BuildContextMaps(
	requested []PresentationContextItem,
	results []PresentationContextResultItem,
) (accepted, refused map[byte]*PresentationContext) {
	// Build abstract syntax lookup from request. The A-ASSOCIATE-AC identifies a
	// context by ID alone, so the SOP class it refers to is only knowable from
	// what was proposed.
	abstractSyntaxByID := make(map[byte]string)
	proposedTransferSyntaxes := make(map[byte][]string)
	for _, req := range requested {
		abstractSyntaxByID[req.ID] = req.AbstractSyntax
		proposedTransferSyntaxes[req.ID] = req.TransferSyntaxes
	}

	accepted = make(map[byte]*PresentationContext)
	refused = make(map[byte]*PresentationContext)
	for _, res := range results {
		pc := &PresentationContext{
			ID:             res.ID,
			AbstractSyntax: abstractSyntaxByID[res.ID],
			TransferSyntax: res.TransferSyntax,
			Result:         res.Result,
			Proposed:       proposedTransferSyntaxes[res.ID],
		}
		if pc.IsAccepted() {
			accepted[res.ID] = pc
		} else {
			refused[res.ID] = pc
		}
	}
	return accepted, refused
}

// RefusalReason describes why a peer refused a presentation context, in terms of
// what the caller can do about it.
func (pc *PresentationContext) RefusalReason() string {
	switch pc.Result {
	case PCResultAcceptance:
		return "accepted"
	case PCResultUserRejection:
		return "the peer rejected it"
	case PCResultNoReason:
		return "the peer gave no reason"
	case PCResultAbstractSyntaxNotSupported:
		return "the peer does not support that SOP class"
	case PCResultTransferSyntaxNotSupported:
		return "the peer supports the SOP class but none of the transfer syntaxes proposed for it"
	default:
		return fmt.Sprintf("the peer answered with result %d", pc.Result)
	}
}

// FindPresentationContextID returns the presentation context ID for a given
// abstract syntax from the accepted contexts map. Returns 0, false if not found.
func FindPresentationContextID(accepted map[byte]*PresentationContext, abstractSyntax string) (byte, bool) {
	for id, pc := range accepted {
		if pc.AbstractSyntax == abstractSyntax {
			return id, true
		}
	}
	return 0, false
}
