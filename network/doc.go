// Package network provides DICOM networking capabilities implementing the
// DICOM Upper Layer Protocol (DICOM Part 8). It enables communication with
// PACS, modalities, and other DICOM-compliant systems over TCP.
//
// The package provides both client (SCU - Service Class User) and server
// (SCP - Service Class Provider) implementations supporting the following
// DIMSE services:
//
//   - C-ECHO: Verification (ping) to test connectivity
//   - C-STORE: Send/receive DICOM objects
//   - C-FIND: Query for DICOM objects
//   - C-MOVE: Retrieve DICOM objects to a third party, transferred over a new
//     association to the destination AE
//   - C-GET: Retrieve DICOM objects over the same association, transferred as
//     C-STORE sub-operations
//
// SCU (Client) Usage:
//
//	scu := network.NewSCU(network.SCUConfig{
//	    CallingAE: "MY_APP",
//	    CalledAE:  "PACS",
//	    Address:   "pacs.hospital.com:11112",
//	})
//
//	// An association must be established before any operation.
//	if err := scu.Associate(ctx, nil); err != nil {
//	    log.Fatal(err)
//	}
//	defer scu.Release(ctx)
//
//	// Verification
//	err = scu.Echo(ctx)
//
//	// Store a dataset
//	err = scu.Store(ctx, ds)
//
//	// Query — results stream on a channel
//	results, err := scu.Find(ctx, queryDataset)
//	for result := range results {
//	    fmt.Println(result.DataSet)
//	}
//
// SCP (Server) Usage:
//
//	scp := network.NewSCP(network.SCPConfig{
//	    AETitle: "MY_SCP",
//	    Port:    11112,
//	})
//	scp.SetHandler(&MyHandler{})
//	err := scp.ListenAndServe(ctx)
//
// See the package examples for C-GET, extended negotiation, and reading
// association details from a handler's context.
package network
