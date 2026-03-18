# network — DICOM Networking for Go

The `network` package implements the DICOM Upper Layer Protocol (DICOM PS3.7/PS3.8), providing both client (SCU) and server (SCP) capabilities. It is the Go equivalent of Python's [pynetdicom](https://github.com/pydicom/pynetdicom), redesigned with native Go concurrency (goroutines, channels, `context.Context`).

## Features

- **All 11 DIMSE services**: C-ECHO, C-STORE, C-FIND, C-MOVE, C-GET, N-EVENT-REPORT, N-GET, N-SET, N-ACTION, N-CREATE, N-DELETE
- **SCU (client)**: Connect to PACS, modalities, and other DICOM nodes
- **SCP (server)**: Accept associations with goroutine-per-connection concurrency
- **80+ Storage SOP Classes**: CT, MR, US, PET, NM, RT, XR, CR, DX, MG, VL, SR, waveforms, encapsulated documents
- **15 Transfer Syntaxes**: All standard uncompressed and compressed (JPEG, JPEG-LS, JPEG 2000, RLE)
- **TLS encryption**: For HIPAA-compliant communication
- **Extended negotiation**: Async operations, SCP/SCU role selection, user identity (username/password, Kerberos, SAML, JWT)
- **Handler system**: Composable handlers — embed `BaseHandler` and override only what you need
- **File-format agnostic**: Works with any DICOM source (.dcm, .ima, DICOMDIR, raw/extensionless)
- **80 tests** with race detection, including full end-to-end integration tests

## Quick Start

### C-ECHO (Verification / Ping)

```go
ctx := context.Background()

scu := network.NewSCU(network.SCUConfig{
    CallingAE: "MY_APP",
    CalledAE:  "PACS",
    Address:   "pacs.hospital.com:11112",
})

err := scu.Associate(ctx, network.DefaultVerificationContexts())
if err != nil {
    log.Fatal(err)
}
defer scu.Release(ctx)

err = scu.Echo(ctx)  // Success = server is reachable
```

### C-STORE (Send DICOM Data)

```go
// dataset can come from any source: .dcm, .ima, DICOMDIR, in-memory
err := scu.Associate(ctx, nil) // nil = propose all default contexts
if err != nil {
    log.Fatal(err)
}
defer scu.Release(ctx)

err = scu.Store(ctx, dataset)
```

### C-FIND (Query)

```go
// Build query
query := dataset.NewDataset()
query.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY")))
query.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Smith*")))
query.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte{})) // request Patient ID

// Results stream on a Go channel
results, err := scu.Find(ctx, query)
if err != nil {
    log.Fatal(err)
}

for result := range results {
    if result.Err != nil {
        log.Printf("error: %v", result.Err)
        break
    }
    fmt.Println(result.DataSet) // each matching study
}
```

### C-MOVE (Retrieve)

```go
err = scu.Move(ctx, queryDataset, "DEST_AE")
```

### SCP Server (Receive Files)

```go
scp := network.NewSCP(network.SCPConfig{
    AETitle: "MY_SCP",
    Port:    11112,
})

scp.SetHandler(&network.StorageHandler{
    OnStore: func(ctx context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
        // Save to disk, database, forward to another PACS, etc.
        log.Printf("Received %s", sopInstance)
        return network.StatusSuccess
    },
})

// Blocks. Each incoming association runs in its own goroutine.
// Cancel ctx for graceful shutdown.
log.Fatal(scp.ListenAndServe(ctx))
```

### TLS Encrypted Communication

```go
// SCU with TLS
transport, err := network.DialTLS(ctx, "pacs:2762", 30*time.Second, &network.TLSConfig{
    CertFile:   "client.crt",
    KeyFile:    "client.key",
    ServerName: "pacs.hospital.com",
})

// SCP with TLS
ln, err := network.ListenTLS("0.0.0.0:2762", &network.TLSConfig{
    CertFile: "server.crt",
    KeyFile:  "server.key",
})
```

## Handler Patterns

The `Handler` interface defines methods for all DIMSE services. Embed `BaseHandler` and override only what you need:

### Echo-Only Server

```go
scp.SetHandler(&network.EchoHandler{})
```

### Storage Server with Callback

```go
scp.SetHandler(&network.StorageHandler{
    OnStore: func(ctx context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
        // Works with ALL DICOM modalities: CT, MR, US, XR, PET, RT, SR, etc.
        // Works with ALL file types: .dcm, .ima, DICOMDIR, raw DICOM
        return network.StatusSuccess
    },
})
```

### Query/Retrieve Server

```go
scp.SetHandler(&network.QueryRetrieveHandler{
    OnFind: func(ctx context.Context, sopClass string, query *dataset.Dataset) ([]*dataset.Dataset, error) {
        // Search your database, return matching results
        return results, nil
    },
    OnMove: func(ctx context.Context, sopClass, dest string, query *dataset.Dataset) error {
        // Send matching instances to the destination AE
        return nil
    },
})
```

### Modality Worklist Server

```go
scp.SetHandler(&network.WorklistHandler{
    OnWorklist: func(ctx context.Context, query *dataset.Dataset) ([]*dataset.Dataset, error) {
        // Return scheduled procedures for the requesting modality
        return procedures, nil
    },
})
```

### Composite Handler (Mix & Match)

```go
h := network.NewCompositeHandler()
h.SetStoreHandler(myStoreHandler)
h.SetFindHandler(myFindHandler)
scp.SetHandler(h)
```

### Custom Handler (Full Control)

```go
type MyHandler struct {
    network.BaseHandler // provides defaults for unimplemented methods
}

func (h *MyHandler) HandleCStore(ctx context.Context, req *network.CStoreRequest) (*network.CStoreResponse, error) {
    log.Printf("Received %s from %s", req.AffectedSOPInstance, req.AffectedSOPClass)
    // Custom processing...
    return &network.CStoreResponse{
        MessageIDRespondedTo: req.MessageID,
        AffectedSOPClass:     req.AffectedSOPClass,
        AffectedSOPInstance:  req.AffectedSOPInstance,
        Status:               network.StatusSuccess,
    }, nil
}

func (h *MyHandler) HandleNCreate(ctx context.Context, req *network.NCreateRequest) (*network.NCreateResponse, error) {
    // Handle MPPS N-CREATE, Print N-CREATE, etc.
    return &network.NCreateResponse{
        MessageIDRespondedTo: req.MessageID,
        AffectedSOPClass:     req.AffectedSOPClass,
        AffectedSOPInstance:  req.AffectedSOPInstance,
        Status:               network.StatusSuccess,
    }, nil
}
```

## CLI Tools

The library includes CLI tools equivalent to pynetdicom's command-line utilities:

```bash
# Build
go build -o dicom .

# Verification (ping)
./dicom echoscu pacs.hospital.com:11112

# Send DICOM files (any format: .dcm, .ima, etc.)
./dicom storescu -aec PACS pacs:11112 study/*.dcm

# Receive DICOM files
./dicom storescp -port 11112 -output ./received/

# Query for studies
./dicom findscu -patient-name "Smith*" -level STUDY pacs:11112

# Retrieve studies
./dicom movescu -dest MY_SCP -study 1.2.3.4 pacs:11112
```

## Architecture

```
network/
├── doc.go                  # Package documentation
├── config.go               # NetworkConfig, SCUConfig, SCPConfig
├── errors.go               # PDUError, AssociationError, TimeoutError, CommunicationError, DIMSEError
├── pdu.go                  # PDU types and encoding/decoding (A-ASSOCIATE, P-DATA, A-RELEASE, A-ABORT)
├── transport.go            # TCP connection management with context-aware timeouts
├── tls.go                  # TLS encryption (DialTLS, ListenTLS)
├── presentation.go         # Transfer syntax UIDs, presentation context negotiation
├── sopclass.go             # 80+ Storage SOP Classes, Q/R, Worklist, MPPS, Print
├── extended.go             # Extended negotiation (async ops, role selection, user identity)
├── association.go          # Association state machine (DICOM Part 8)
├── dimse.go                # C-DIMSE message types (C-ECHO, C-STORE, C-FIND, C-MOVE, C-GET)
├── ndimse.go               # N-DIMSE message types (N-EVENT-REPORT, N-GET, N-SET, N-ACTION, N-CREATE, N-DELETE)
├── handlers.go             # Handler interfaces + BaseHandler, EchoHandler, StorageHandler, etc.
├── scu.go                  # Service Class User (client)
├── scp.go                  # Service Class Provider (server)
├── *_test.go               # 80 tests including integration tests
└── README.md               # This file
```

## Supported SOP Classes

### Storage (80+)

CT, Enhanced CT, MR, Enhanced MR, MR Spectroscopy, US, Enhanced US, CR, DX, Digital Mammography, Intra-oral XR, Secondary Capture (all multi-frame variants), Nuclear Medicine, PET, Enhanced PET, RT Image/Dose/Structure/Plan/Beams/Ion, XA, Enhanced XA, XRF, Enhanced XRF, 3D Angiographic, Breast Tomosynthesis, VL Endoscopic/Microscopic/Photographic, Video, Ophthalmic Photography/Tomography, Whole Slide Microscopy, ECG (12-lead, General, Ambulatory), Hemodynamic, Cardiac EP, Arterial Pulse, Respiratory, Audio, EMG, EEG, Body Position, Basic/Enhanced/Comprehensive SR, Procedure Log, CAD SR (Mammography, Chest, Colon), Key Object Selection, Radiation Dose SR, Presentation States (Grayscale, Color, Pseudo-Color, Blending), Segmentation, Surface Segmentation, Parametric Map, Raw Data, Spatial Registration/Fiducials, Encapsulated PDF/CDA/STL/OBJ/MTL

### Query/Retrieve

Patient Root Q/R (Find, Move, Get), Study Root Q/R (Find, Move, Get)

### Worklist & Procedure Step

Modality Worklist (MWL) Find, Modality Performed Procedure Step (N-CREATE, N-SET), Unified Procedure Step (Push, Watch, Pull, Event, Query)

### Print Management

Basic Film Session, Basic Film Box, Basic Grayscale/Color Image Box, Print Job, Grayscale/Color Print Management, Printer, Printer Configuration Retrieval

### Other

Storage Commitment (Push Model), Instance Availability Notification, Substance Administration Logging, Hanging Protocol/Color Palette/Implant Template Storage

## Comparison with pynetdicom

| Feature | pynetdicom | go-dicom/network |
|---------|-----------|-----------------|
| Language | Python | Go |
| Concurrency | threading | goroutines (lightweight, scalable) |
| Result streaming | callbacks | Go channels |
| Cancellation | N/A | `context.Context` (timeouts, graceful shutdown) |
| Association handling | thread pool | goroutine-per-association |
| C-DIMSE | 5 services | 5 services |
| N-DIMSE | 6 services | 6 services |
| TLS | ssl.SSLContext | crypto/tls |
| CLI tools | 7 | 5 (echoscu, storescu, storescp, findscu, movescu) |
| Event system | evt_handlers | Handler interface with BaseHandler embedding |
| Testing | pytest | go test -race (80 tests, 0 race conditions) |

## Testing

```bash
# Unit + integration tests
go test -v ./network/...

# With race detection
go test -race ./network/...

# Integration tests only (real TCP SCP+SCU)
go test -v -run TestIntegration ./network/...

# Specific test
go test -v -run TestIntegrationCStoreRoundTrip ./network/...
```

## Status Codes

The package defines standard DICOM status codes:

| Constant | Value | Meaning |
|----------|-------|---------|
| `StatusSuccess` | 0x0000 | Operation completed successfully |
| `StatusPending` | 0xFF00 | More results to follow |
| `StatusPendingWarning` | 0xFF01 | More results, with warnings |
| `StatusCancel` | 0xFE00 | Operation cancelled |
| `StatusWarning` | 0x0001 | Coercion warning |
| `StatusOutOfResources` | 0xA700 | Out of resources |
| `StatusUnableToProcess` | 0xC000 | Unable to process |
| `StatusMoveDestUnknown` | 0xA801 | Move destination unknown |
| `StatusClassNotSupported` | 0x0122 | SOP Class not supported |

## References

- [DICOM PS3.7 — Message Exchange](https://dicom.nema.org/medical/dicom/current/output/html/part07.html)
- [DICOM PS3.8 — Network Communication Support](https://dicom.nema.org/medical/dicom/current/output/html/part08.html)
- [pynetdicom Documentation](https://pydicom.github.io/pynetdicom/)
- [pynetdicom GitHub](https://github.com/pydicom/pynetdicom)
