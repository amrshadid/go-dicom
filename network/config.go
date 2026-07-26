package network

import (
	"context"
	"time"

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

	// ARTIMTimeout is the timeout for the ARTIM timer.
	ARTIMTimeout time.Duration

	// DIMSETimeout is the timeout for DIMSE operations.
	DIMSETimeout time.Duration

	// NetworkTimeout is the timeout for establishing TCP connections.
	NetworkTimeout time.Duration
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

	// Port is the TCP port to listen on.
	Port int

	// BindAddress is the address to bind to. Empty string means all interfaces.
	BindAddress string

	// Network holds low-level network settings.
	Network NetworkConfig

	// MaxAssociations is the maximum number of concurrent associations. 0 means unlimited.
	MaxAssociations int
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
