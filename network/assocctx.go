package network

import (
	"context"
	"net"
)

// AssociationInfo holds association-level information that is made available
// to Handler methods via the context. This allows handlers to access details
// about the current association (e.g., who is connecting) without changing
// the Handler interface.
type AssociationInfo struct {
	// CallingAE is the AE title of the remote peer (the SCU).
	CallingAE string

	// CalledAE is the AE title that was called (the SCP).
	CalledAE string

	// RemoteAddr is the network address of the remote peer.
	RemoteAddr net.Addr

	// LocalAddr is the local network address of this side of the connection.
	LocalAddr net.Addr

	// MaxPDUSize is the negotiated maximum PDU size for the association.
	MaxPDUSize uint32

	// AcceptedContexts contains the negotiated presentation contexts,
	// keyed by presentation context ID.
	AcceptedContexts map[byte]*PresentationContext

	// PeerImplementationClassUID is the implementation class UID reported
	// by the remote peer in the A-ASSOCIATE-RQ.
	PeerImplementationClassUID string

	// PeerImplementationVersion is the implementation version name reported
	// by the remote peer in the A-ASSOCIATE-RQ.
	PeerImplementationVersion string
}

type assocCtxKey struct{}

// ContextWithAssociationInfo returns a new context with the given AssociationInfo attached.
func ContextWithAssociationInfo(ctx context.Context, info *AssociationInfo) context.Context {
	return context.WithValue(ctx, assocCtxKey{}, info)
}

// AssociationInfoFromContext extracts the AssociationInfo from the context.
// Returns nil if no association info is present.
func AssociationInfoFromContext(ctx context.Context) *AssociationInfo {
	info, _ := ctx.Value(assocCtxKey{}).(*AssociationInfo)
	return info
}
