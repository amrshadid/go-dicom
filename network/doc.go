// Package network implements DICOM networking: the Upper Layer Protocol from
// PS3.8, the DIMSE message exchange from PS3.7, and the service classes that
// run over them. It is what lets a program talk to a PACS, a modality, or any
// other DICOM peer over TCP.
//
// Both roles are provided: a Service Class User ([SCU], the client that
// initiates) and a Service Class Provider ([SCP], the server that responds).
//
// # Concepts
//
// Four terms carry most of the weight, and the API mirrors them directly:
//
//   - Application Entity (AE) title — the name a DICOM node answers to. Peers
//     are addressed by AE title as well as host and port, and an SCP rejects an
//     association whose called AE title is not its own.
//   - Association — the negotiated session. Everything happens inside one; it
//     is established, used, then released.
//   - Presentation context — a pairing of an abstract syntax (a SOP Class: what
//     kind of object) with a transfer syntax (how it is encoded). Both sides
//     negotiate a set at association time, and every message travels on one.
//   - DIMSE service — the operation itself: C-ECHO, C-STORE, C-FIND, and so on.
//
// # Client
//
// Associate, issue operations, release. The association must exist before any
// operation, and be released when finished.
//
//	scu := network.NewSCU(network.SCUConfig{
//		CallingAE: "MY_APP",
//		CalledAE:  "PACS",
//		Address:   "pacs.hospital.com:11112",
//	})
//
//	if err := scu.Associate(ctx, nil); err != nil {
//		log.Fatal(err)
//	}
//	defer scu.Release(ctx)
//
//	// Verification: confirms the peer is reachable and accepts the AE title.
//	if err := scu.Echo(ctx); err != nil {
//		log.Fatal(err)
//	}
//
//	// Send an instance. The data set must carry SOP Class UID (0008,0016) and
//	// SOP Instance UID (0008,0018); the presentation context is chosen from
//	// the former.
//	if err := scu.Store(ctx, ds); err != nil {
//		log.Fatal(err)
//	}
//
// Passing nil for the presentation contexts proposes a default set covering
// verification, common storage SOP classes, and query/retrieve. Pass an
// explicit slice to narrow or extend that.
//
// Query results stream on a channel, since a C-FIND can match arbitrarily many
// instances and the peer sends them one at a time:
//
//	results, err := scu.Find(ctx, queryDS)
//	if err != nil {
//		log.Fatal(err)
//	}
//	for r := range results {
//		if r.Err != nil {
//			return r.Err
//		}
//		process(r.DataSet)
//	}
//
// The channel closes when the peer sends its final status. Always drain it or
// cancel the context, or the receiving goroutine blocks.
//
// # Retrieval
//
// C-GET and C-MOVE both retrieve instances; they differ in where the data goes.
//
// C-GET returns instances over the same association, as C-STORE
// sub-operations. Set SCUConfig.OnCStore to receive them — without it they are
// acknowledged and discarded, and the retrieval completes having kept nothing:
//
//	scu := network.NewSCU(network.SCUConfig{
//		CallingAE: "MY_APP", CalledAE: "PACS", Address: addr,
//		OnCStore: func(ctx context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
//			save(ds)
//			return network.StatusSuccess
//		},
//	})
//	err := scu.Get(ctx, queryDS)
//
// C-MOVE sends them to a third party named only by AE title, over a separate
// association, while the requestor watches the counts:
//
//	err := scu.Move(ctx, queryDS, "WORKSTATION")
//
// An SCP performing C-MOVE must be able to resolve that title to an address
// through SCPConfig.MoveDestinations or SCPConfig.ResolveMoveDestination, or it
// answers with [StatusMoveDestUnknown].
//
// # Server
//
// An SCP listens, negotiates, and dispatches each request to a [Handler].
// ListenAndServe blocks, handling each association in its own goroutine.
//
//	scp := network.NewSCP(network.SCPConfig{
//		AETitle:         "MY_SCP",
//		Port:            11112,
//		MaxAssociations: 32,
//	})
//
//	scp.SetHandler(&network.StorageHandler{
//		OnStore: func(ctx context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
//			return network.StatusSuccess
//		},
//	})
//
//	log.Fatal(scp.ListenAndServe(ctx))
//
// The handler runs on the association's goroutine, so a slow handler holds that
// association open; hand long work to a queue. Handlers may be called
// concurrently for different associations and must be safe for concurrent use.
//
// Ready-made handlers cover the common shapes: [EchoHandler] for verification,
// [StorageHandler] for C-STORE, [QueryRetrieveHandler] for C-FIND, C-MOVE, and
// C-GET, [WorklistHandler] for Modality Worklist, and [CompositeHandler] to
// combine separate handlers per service. For full control, implement [Handler]
// directly, embedding [BaseHandler] to inherit defaults for the rest.
//
// [AssociationInfoFromContext] gives a handler the calling AE title, peer
// address, negotiated contexts, and peer implementation details without
// changing the Handler signature. It returns nil outside an association.
//
// # Statuses
//
// A DIMSE response carries a 16-bit status, not an error. [StatusSuccess] means
// the operation completed, [StatusPending] that more responses follow, and
// values such as [StatusOutOfResources] or [StatusUnableToProcess] indicate
// failure. Handlers return a status, and rejecting a request by returning a
// failure status is normal rather than exceptional.
//
// # Transfer syntaxes
//
// Data sets are encoded using the syntax negotiated for the presentation
// context they travel on, not a fixed one. [EncodeDataset] and [DecodeDataset]
// expose that directly, and [Association.TransferSyntaxFor] reports what was
// agreed for a context. Implicit VR Little Endian, Explicit VR Little Endian,
// Explicit VR Big Endian, and Deflated Explicit VR Little Endian are handled;
// compressed syntaxes carry an Explicit VR Little Endian data set with
// compressed pixel data.
//
// # Extended negotiation
//
// Optional items negotiated alongside the presentation contexts, supplied via
// SCUConfig.ExtendedNegotiation: an asynchronous operations window, SCP/SCU
// role selection, and user identity (username and password, Kerberos, SAML, or
// JWT). Role selection is what permits a peer to send C-STORE back over an
// association this AE initiated, which C-GET requires.
//
// # Security
//
// DICOM over TCP is unauthenticated and unencrypted by default. Use [DialTLS]
// and [ListenTLS] for encrypted transport. Checking the called AE title is a
// naming convention, not authentication — anything reachable on the port can
// claim any calling AE title unless TLS with client certificates or user
// identity negotiation is in use. Set SCPConfig.MaxAssociations to bound
// concurrency on an exposed server.
//
// Decoders reject input that would otherwise exhaust memory: PDU lengths are
// capped at [MaxPDULengthLimit], deflated data sets at
// [MaxInflatedDatasetSize], and element lengths are checked against the bytes
// actually available. The parsers have fuzz targets that run in CI.
//
// # Logging
//
// Everything this package reports goes through [DefaultLogger], which writes to
// config.Logger — so one call configures the whole library, and a program
// embedding an SCP can redirect or silence it:
//
//	config.SetLogger(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
//
// Messages carry a component=network attribute, so a shared logger can filter
// this package in or out. To change the verbosity of the network layer alone,
// without touching the rest of the library:
//
//	network.SetDefaultLogLevel(network.LogLevelSilent) // report nothing
//	network.DebugLogger()                              // everything, to stderr
//
// The default is [LogLevelWarn], matching config.Logger's own default: failures
// and refusals are reported, PDU and DIMSE detail is not.
//
// # Limitations
//
//   - As an SCP, dispatch is one message at a time per association, so
//     MaxOperationsPerformed does not bound concurrent dispatch. An SCU enforces
//     its own window; a server answers in the order received.
//   - RLE Lossless is the only syntax pixel data is compressed *to*. It is
//     transcoded in both directions as the negotiated context requires, but a
//     context needing any other compressed target fails rather than sending
//     bytes described as something they are not.
//   - C-MOVE and C-GET take the association exclusively, so neither overlaps with
//     another operation. Both interleave traffic that is not their own response —
//     a C-GET receives C-STORE sub-operation requests, a C-MOVE has a cancel
//     watcher reading the association.
package network
