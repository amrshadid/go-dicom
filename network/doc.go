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
//   - C-MOVE: Retrieve DICOM objects via sub-operations
//   - C-GET: Retrieve DICOM objects on the same association
//
// SCU (Client) Usage:
//
//	scu, err := network.NewSCU(network.SCUConfig{
//	    CallingAE: "MY_APP",
//	    CalledAE:  "PACS",
//	    Address:   "pacs.hospital.com:11112",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer scu.Release(ctx)
//
//	// Verification
//	err = scu.Echo(ctx)
//
//	// Store a dataset
//	err = scu.Store(ctx, dataset)
//
//	// Query
//	results, err := scu.Find(ctx, queryDataset)
//	for result := range results {
//	    fmt.Println(result)
//	}
//
// SCP (Server) Usage:
//
//	scp, err := network.NewSCP(network.SCPConfig{
//	    AETitle: "MY_SCP",
//	    Port:    11112,
//	})
//	scp.SetHandler(&MyHandler{})
//	err = scp.ListenAndServe(ctx)
package network
