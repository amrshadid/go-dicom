package network

import (
	"context"
	"log/slog"
	"time"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataset"
)

const (
	// DefaultMaxPDUSize is the default maximum PDU size (16 KB).
	DefaultMaxPDUSize = 16384

	// DefaultPort is the default DICOM port.
	DefaultPort = 11112

	// DefaultAETitle is the default Application Entity title.
	DefaultAETitle = "GODICOM"

	// DefaultARTIMTimeout is the ARTIM timer timeout (Association Request/Reject/Release Timer).
	DefaultARTIMTimeout = 30 * time.Second

	// DefaultDIMSETimeout is the default timeout for DIMSE operations.
	DefaultDIMSETimeout = 60 * time.Second

	// DefaultNetworkTimeout is the default TCP connection timeout.
	DefaultNetworkTimeout = 30 * time.Second

	// MaxMaxPDUSize is the absolute maximum PDU size allowed.
	MaxMaxPDUSize = 0 // 0 means no limit (per DICOM standard, negotiated)

	// MinPDUSize is the minimum PDU size required by the standard.
	MinPDUSize = 4096

	// ProtocolVersion is the DICOM Upper Layer protocol version.
	ProtocolVersion uint16 = 1
)

// NetworkConfig holds configuration for DICOM network operations.
type NetworkConfig struct {
	// MaxPDUSize is the maximum PDU size to propose during association negotiation.
	MaxPDUSize uint32

	// ARTIMTimeout is the ARTIM timer (PS3.8 9.1.5): how long an SCP waits
	// for the A-ASSOCIATE-RQ on a connection it accepted, and an SCU for the
	// A-ASSOCIATE-AC or A-RELEASE-RP. Zero is the default; less than zero is
	// no limit.
	ARTIMTimeout time.Duration

	// DIMSETimeout is how long each PDU of an established association has to
	// arrive: an SCU waiting for a response, or an SCP waiting for the next
	// request. An SCP aborts an association that is idle for longer. Zero is
	// the default; less than zero is no limit.
	DIMSETimeout time.Duration

	// NetworkTimeout is the timeout for establishing TCP connections.
	NetworkTimeout time.Duration

	// JPEGQuality allows an image to be sent as JPEG Baseline or JPEG
	// Extended, at this quality from 1 to 100, when that is the only syntax
	// the peer accepts for it. Both are lossy, so nothing is sent that way
	// unless this is set: zero refuses, and the send fails. JPEG Baseline
	// applies to 8-bit images; JPEG Extended to unsigned images of 8 bits,
	// or of no more than 12 stored in 16. Anything else is refused whatever
	// this says.
	JPEGQuality int

	// JPEGLSNear is the allowed error, NEAR of ITU-T T.87, for an image sent
	// as JPEG-LS Lossless and Near-Lossless (1.2.840.10008.1.2.4.81) when that
	// is the only syntax the peer accepts for it: every sample arrives within
	// this much of its value. Zero, the default, codes the image losslessly
	// under that syntax, which it allows, so nothing is lost unasked. A signed
	// image is sent only at zero. It runs from 0 to 255.
	JPEGLSNear int

	// JPEG2000Rate allows an image to be sent as lossy JPEG 2000
	// (1.2.840.10008.1.2.4.91) when that is the only syntax the peer accepts
	// for it, at this compression ratio or this PSNR, one of them. It is
	// lossy, so nothing is sent that way unless this is set: the zero value
	// refuses, and the send fails.
	JPEG2000Rate compress.JPEG2000Rate

	// MaxDataSetSize bounds the data set of one message, which is held in
	// memory whole while it arrives. A peer sending more is aborted. Zero is
	// no limit. A C-STORE given to a CStoreStreamer is not held whole; the
	// bound still applies to it, counted as its bytes are read.
	MaxDataSetSize int64
}

// DefaultNetworkConfig returns a NetworkConfig with sensible defaults.
func DefaultNetworkConfig() NetworkConfig {
	return NetworkConfig{
		MaxPDUSize:     DefaultMaxPDUSize,
		ARTIMTimeout:   DefaultARTIMTimeout,
		DIMSETimeout:   DefaultDIMSETimeout,
		NetworkTimeout: DefaultNetworkTimeout,
	}
}

// SCUConfig holds configuration for a Service Class User (client).
type SCUConfig struct {
	// CallingAE is the AE title of this SCU (the client).
	CallingAE string

	// CalledAE is the AE title of the target SCP (the server).
	CalledAE string

	// Address is the target address in "host:port" format.
	Address string

	// Network holds low-level network settings.
	Network NetworkConfig

	// ExtendedNegotiation carries optional A-ASSOCIATE-RQ extended negotiation
	// items: asynchronous operations window, SCP/SCU role selection, and user
	// identity (username/password, Kerberos, SAML, JWT). Nil proposes none.
	ExtendedNegotiation *ExtendedNegotiation

	// TLS, when set, makes Associate connect over TLS.
	TLS *TLSConfig

	// Events, when set, is told what happens on each association: every
	// PDU, the association's own events and every DIMSE message. See
	// EventType.
	Events *EventManager

	// Logger, when set, is where everything this SCU logs goes: its
	// associations, their negotiation, and a panic in a handler of its
	// Events. Nil means the package's DefaultLogger, as before. A logger
	// given here receives every line at every level; filter with its
	// handler's level.
	Logger *slog.Logger

	// OnCStore receives instances the peer sends as C-STORE sub-operations
	// during a C-GET. Return a DIMSE status; StatusSuccess accepts the
	// instance.
	//
	// When nil, incoming instances are acknowledged with StatusSuccess and
	// discarded — the C-GET still completes, but nothing is retained. Set this
	// to actually keep what a C-GET retrieves.
	OnCStore CStoreSubOperationFunc
}

// CStoreSubOperationFunc handles an instance pushed by a peer as a C-STORE
// sub-operation during a C-GET.
type CStoreSubOperationFunc func(ctx context.Context, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) uint16

// SCPConfig holds configuration for a Service Class Provider (server).
type SCPConfig struct {
	// AETitle is the AE title of this SCP.
	AETitle string

	// Port is the TCP port to listen on. NewSCP reads 0 as DefaultPort;
	// StartServer and StartServerTLS read it as any free port.
	Port int

	// BindAddress is the address to bind to. Empty string means all interfaces.
	BindAddress string

	// Network holds low-level network settings.
	Network NetworkConfig

	// MaxAssociations is the maximum number of concurrent associations. 0 means unlimited.
	// A caller over the limit is refused rejected-transient, by the
	// service-provider (presentation related function), local-limit-exceeded
	// (PS3.8 Table 9-21), so it knows to try again later.
	MaxAssociations int

	// Admit, when set, decides who may associate. It is called once per
	// association, after the A-ASSOCIATE-RQ is read and before it is answered
	// or any presentation context is negotiated, so a caller it refuses
	// learns nothing of what the SCP supports. It returns nil to admit the
	// caller, or the A-ASSOCIATE-RJ to refuse it with; RejectCallingAE,
	// RejectCalledAE and RejectBusy are the usual ones.
	//
	// It is asked only about a request whose two AE titles are well formed
	// (PS3.5 6.2: 1 to 16 printable characters of the default repertoire, no
	// backslash, not all spaces), and is given them without their leading and
	// trailing spaces. A request with a title that is not is refused by the
	// SCP, as calling-AE-title-not-recognized or called-AE-title-not-recognized,
	// without the function being asked: an allow list would refuse such a
	// title anyway, and a deny list or a log line should never meet one.
	//
	// The called AE title is still the SCP's to check. A caller the function
	// admits is refused, called-AE-title-not-recognized, when it asked for a
	// title other than AETitle, so adding a function that only observes does
	// not open the SCP to every title. Set AcceptAnyCalledAE for the function
	// to decide the called title as well.
	//
	// It runs on a goroutine of its own and has ARTIMTimeout to answer; its
	// context is canceled then and the caller refused with RejectBusy. ARTIM
	// also bounds how long the caller takes to send its request, so a
	// connection can be held for up to twice ARTIM before it is answered. A
	// function that ignores its context keeps its goroutine until it returns.
	// A panic in it refuses the caller (rejected-permanent, service-user,
	// no-reason-given) and is logged. So is a result, source and reason that
	// PS3.8 Table 9-21 does not define, which is sent as that refusal
	// instead. Each refusal is logged at the info level, the AE titles
	// quoted, and reported to Events as EVTAssocRejected, whose Description
	// names it. LoggingEventHandlers on the same log leaves out a refusal
	// the SCP's line has already put there, so it appears once; at the
	// default warning level, where the SCP's line is not written, theirs is
	// the one that appears.
	// Callers turned away by MaxAssociations are refused before it is asked.
	//
	// It is called concurrently for different connections and must be safe
	// for concurrent use.
	Admit func(ctx context.Context, req *AssociationRequest) *AssociationRejection

	// AcceptAnyCalledAE leaves the called AE title to Admit: the SCP does
	// not refuse a title other than AETitle by itself, so one SCP can answer
	// to several. Admit then refuses the titles it does not answer to, with
	// RejectCalledAE. It has no effect without Admit: an SCP with no
	// admission function answers only to AETitle.
	AcceptAnyCalledAE bool

	// Events, when set, is told what happens on each association the SCP
	// accepts, and on those it opens itself: C-MOVE sub-operations, storage
	// commitment results and UPS event reports. A Server started with it
	// uses it as its Events.
	Events *EventManager

	// AbstractSyntaxes are the SOP classes the SCP accepts. They are read by
	// NewSCP and StartServer before the listener opens, so no association is
	// ever negotiated with a wider set, which setting them afterwards with
	// SetSupportedAbstractSyntaxes cannot promise: the accept loop is already
	// running when StartServer returns.
	//
	// Nil and empty are different. Nil keeps the defaults, which include
	// query and retrieve (Patient Root, Study Root and Patient/Study Only
	// FIND, MOVE and GET) as well as verification, the common storage
	// classes and storage commitment. A non-nil list is used exactly as
	// given, so an empty one accepts nothing. StorageAndVerificationSyntaxes
	// is the list for a storage-only archive.
	AbstractSyntaxes []string

	// TransferSyntaxes are the transfer syntaxes the SCP accepts, read when
	// AbstractSyntaxes are. Nil keeps the defaults (DefaultTransferSyntaxes);
	// a non-nil list is used exactly as given, so an empty one accepts no
	// presentation context at all.
	TransferSyntaxes []string

	// Logger, when set, is where everything this SCP logs goes: the
	// associations it accepts, those it opens itself (C-MOVE sub-operations,
	// storage commitment results, UPS event reports), its admission
	// decisions, a panic in a handler or in a handler of its Events, and, for
	// a Server, the start-up line and accept errors (Server.Logger writes
	// here). Nil means the package's DefaultLogger, as before, and
	// Server.Logger keeps its own default. A logger given here receives every
	// line at every level; filter with its handler's level. The lines name AE
	// titles, addresses and SOP Instance UIDs, which is why a server may want
	// them kept apart from another's.
	Logger *slog.Logger

	// TLS, when set, makes ListenAndServe and StartServer accept associations
	// over TLS only.
	TLS *TLSConfig

	// OutboundTLS, when set, is used for the associations the SCP opens
	// itself: C-MOVE sub-operations, storage commitment results and UPS event
	// reports. It is separate from TLS because a peer the SCP calls need not
	// be reached the way the SCP is.
	OutboundTLS *TLSConfig

	// MoveDestinations maps a destination AE title to its "host:port" address,
	// for resolving the Move Destination of a C-MOVE request.
	//
	// C-MOVE sends the retrieved instances to a third party rather than back to
	// the requestor, and the request names that party only by AE title. Without
	// a way to resolve it there is nowhere to send them, and the SCP answers
	// with StatusMoveDestUnknown.
	MoveDestinations map[string]string

	// ResolveMoveDestination resolves a destination AE title to a "host:port"
	// address. It takes precedence over MoveDestinations, for callers that look
	// destinations up in a database or configuration service.
	ResolveMoveDestination func(aeTitle string) (address string, ok bool)

	// CommitmentRequestors maps a requestor's AE title to its "host:port"
	// address, for reporting a storage commitment result on a new association.
	//
	// PS3.4 §J.3 allows the N-EVENT-REPORT to arrive long after the N-ACTION,
	// which is what an archive that verifies durability before promising it has
	// to do. By then the original association is gone, so the SCP has to reach
	// the requestor itself — and a request names its sender only by Calling AE
	// title. Without a way to resolve it there is nowhere to send the result.
	CommitmentRequestors map[string]string

	// ResolveCommitmentRequestor resolves a requestor AE title to a "host:port"
	// address, taking precedence over CommitmentRequestors.
	ResolveCommitmentRequestor func(aeTitle string) (address string, ok bool)
}

// resolveCommitmentRequestor maps a requestor AE title to an address,
// preferring the resolver function over the static map.
func (c *SCPConfig) resolveCommitmentRequestor(aeTitle string) (string, bool) {
	if c.ResolveCommitmentRequestor != nil {
		if addr, ok := c.ResolveCommitmentRequestor(aeTitle); ok {
			return addr, true
		}
	}
	addr, ok := c.CommitmentRequestors[aeTitle]
	return addr, ok
}

// resolveMoveDestination maps a destination AE title to an address, preferring
// the resolver function over the static map.
func (c *SCPConfig) resolveMoveDestination(aeTitle string) (string, bool) {
	if c.ResolveMoveDestination != nil {
		if addr, ok := c.ResolveMoveDestination(aeTitle); ok {
			return addr, true
		}
		return "", false
	}
	addr, ok := c.MoveDestinations[aeTitle]
	return addr, ok
}

// applyDefaults fills in any zero-valued config fields with defaults.
func (c *SCUConfig) applyDefaults() {
	if c.CallingAE == "" {
		c.CallingAE = DefaultAETitle
	}
	if c.Network.MaxPDUSize == 0 {
		c.Network.MaxPDUSize = DefaultMaxPDUSize
	}
	if c.Network.ARTIMTimeout == 0 {
		c.Network.ARTIMTimeout = DefaultARTIMTimeout
	}
	if c.Network.DIMSETimeout == 0 {
		c.Network.DIMSETimeout = DefaultDIMSETimeout
	}
	if c.Network.NetworkTimeout == 0 {
		c.Network.NetworkTimeout = DefaultNetworkTimeout
	}
}

// applyDefaults fills in any zero-valued config fields with defaults.
func (c *SCPConfig) applyDefaults() {
	if c.AETitle == "" {
		c.AETitle = DefaultAETitle
	}
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.Network.MaxPDUSize == 0 {
		c.Network.MaxPDUSize = DefaultMaxPDUSize
	}
	if c.Network.ARTIMTimeout == 0 {
		c.Network.ARTIMTimeout = DefaultARTIMTimeout
	}
	if c.Network.DIMSETimeout == 0 {
		c.Network.DIMSETimeout = DefaultDIMSETimeout
	}
	if c.Network.NetworkTimeout == 0 {
		c.Network.NetworkTimeout = DefaultNetworkTimeout
	}
}
