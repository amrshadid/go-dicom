package network

// AllTransferSyntaxes returns every transfer syntax this library will negotiate,
// uncompressed first.
//
// Opt in with SetSupportedTransferSyntaxes on an SCP, or by building
// presentation contexts with it on an SCU. It is not the default for the same
// reason it is not pynetdicom's: a context list grows with the product of SOP
// classes and transfer syntaxes, and proposing every combination makes an
// association request large enough to matter.
//
// Most of these are not decodable by this library — see CONFORMANCE.md — and
// that is not what negotiating them is for. An archive or router receives an
// instance, stores it, and forwards it later; the pixel data travels as opaque
// bytes and never needs to be understood. Declining these contexts made
// go-dicom unable to receive most real-world imaging, since almost everything a
// modality produces is compressed.
//
// Order is preference order: the uncompressed syntaxes come first, so a peer
// offering both an uncompressed and a compressed form is met with the one this
// library can decode.
func AllTransferSyntaxes() []string {
	out := make([]string, 0, len(DefaultTransferSyntaxes())+len(compressedTransferSyntaxes))
	out = append(out, DefaultTransferSyntaxes()...)
	out = append(out, compressedTransferSyntaxes...)
	return out
}

// compressedTransferSyntaxes carry compressed pixel data. In every one of them
// the data set surrounding the pixel data is explicit VR little endian, so
// decoding the data set never depends on having the codec.
var compressedTransferSyntaxes = []string{
	JPEGBaselineUID,
	JPEGExtendedUID,
	JPEGLosslessSV1UID,
	JPEGLosslessUID,
	JPEGLSLosslessUID,
	JPEGLSNearLosslessUID,
	JPEG2000LosslessUID,
	JPEG2000UID,
	JPEG2000Part2MultiComponentLosslessUID,
	JPEG2000Part2MultiComponentUID,
	JPIPReferencedUID,
	JPIPReferencedDeflateUID,
	MPEG2MainProfileUID,
	MPEG2MainProfileHighUID,
	MPEG2MainProfileHighFragmentUID,
	MPEG4AVCH264HighProfileUID,
	MPEG4AVCH264HighProfileFragmentUID,
	MPEG4AVCH264BDCompatibleUID,
	MPEG4AVCH264BDCompatibleFragmentUID,
	MPEG4AVCH264HighProfile2DUID,
	MPEG4AVCH264HighProfile2DFragmentUID,
	MPEG4AVCH264HighProfile3DUID,
	MPEG4AVCH264HighProfile3DFragmentUID,
	MPEG4AVCH264StereoHighProfileUID,
	MPEG4AVCH264StereoHighFragmentUID,
	HEVCH265MainProfileUID,
	HEVCH265Main10ProfileUID,
	HTJ2KLosslessUID,
	HTJ2KLosslessRPCLUID,
	HTJ2KUID,
	JPIPHTJ2KReferencedUID,
	JPIPHTJ2KReferencedDeflateUID,
	RLELosslessUID,
}
