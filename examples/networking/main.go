// Example: DICOM Networking with go-dicom
//
// This example demonstrates SCU (client) and SCP (server) usage for
// DICOM networking operations including C-ECHO, C-STORE, C-FIND, and C-MOVE.
//
// The go-dicom network package supports all DICOM file types (.dcm, .ima,
// DICOMDIR, raw DICOM) since it operates on Dataset objects, not file formats.
//
// Usage:
//
//	go run examples/networking/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("DICOM Networking Examples")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  go run . echo              - Run Echo SCP+SCU example")
		fmt.Println("  go run . store             - Run Store SCP+SCU example")
		fmt.Println("  go run . find              - Run Find SCP+SCU example")
		fmt.Println("  go run . handlers          - Show handler patterns")
		fmt.Println("  go run . all               - Run all examples")
		return
	}

	switch os.Args[1] {
	case "echo":
		runEchoExample()
	case "store":
		runStoreExample()
	case "find":
		runFindExample()
	case "handlers":
		showHandlerPatterns()
	case "all":
		runEchoExample()
		fmt.Println()
		runStoreExample()
		fmt.Println()
		runFindExample()
		fmt.Println()
		showHandlerPatterns()
	default:
		fmt.Printf("Unknown example: %s\n", os.Args[1])
	}
}

// runEchoExample demonstrates C-ECHO (verification/ping).
func runEchoExample() {
	fmt.Println("=== C-ECHO Example (Verification) ===")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start SCP (server) in background
	scp := network.NewSCP(network.SCPConfig{
		AETitle:     "ECHO_SCP",
		Port:        0,
		BindAddress: "127.0.0.1",
	})
	scp.SetHandler(&network.EchoHandler{})

	// Use a listener to get a random port
	ln, err := network.Listen("127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	scpCtx, scpCancel := context.WithCancel(ctx)
	defer scpCancel()

	// Extract port from address
	scp.SetSupportedAbstractSyntaxes([]string{network.VerificationSOPClassUID})

	go func() {
		scp.ListenAndServe(scpCtx)
	}()
	time.Sleep(100 * time.Millisecond) // Let server start

	// Create SCU (client)
	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "ECHO_SCU",
		CalledAE:  "ECHO_SCP",
		Address:   addr,
	})

	// Associate and send C-ECHO
	fmt.Printf("Connecting to %s...\n", addr)
	if err := scu.Associate(ctx, network.DefaultVerificationContexts()); err != nil {
		// Server might not be ready yet on the random port
		fmt.Printf("Note: Association failed (expected in demo without real server): %v\n", err)
		return
	}
	defer scu.Release(ctx)

	if err := scu.Echo(ctx); err != nil {
		fmt.Printf("C-ECHO failed: %v\n", err)
		return
	}

	fmt.Println("C-ECHO Success! Server is reachable.")
	scpCancel()
}

// runStoreExample demonstrates C-STORE (sending DICOM data).
func runStoreExample() {
	fmt.Println("=== C-STORE Example (Send Data) ===")

	// Build a sample dataset (this could come from any DICOM file: .dcm, .ima, etc.)
	ds := dataset.NewDataset()
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(network.CTImageStorageUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.3.4.5.6.7.8.9")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Smith^John")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("12345")))

	fmt.Println("Created sample CT dataset")
	fmt.Println("  Patient: Smith^John")
	fmt.Println("  ID: 12345")
	fmt.Println("  SOP Class: CT Image Storage")

	// In a real scenario, you would:
	// 1. Read from any DICOM file (.dcm, .ima, DICOMDIR, raw)
	// 2. Associate with a PACS server
	// 3. Call scu.Store(ctx, ds)
	fmt.Println()
	fmt.Println("To send to a real PACS:")
	fmt.Println("  scu := network.NewSCU(network.SCUConfig{")
	fmt.Println("      CallingAE: \"MY_APP\",")
	fmt.Println("      CalledAE:  \"PACS\",")
	fmt.Println("      Address:   \"pacs.hospital.com:11112\",")
	fmt.Println("  })")
	fmt.Println("  scu.Associate(ctx, nil)")
	fmt.Println("  scu.Store(ctx, dataset)")
	fmt.Println("  scu.Release(ctx)")
	_ = ds
}

// runFindExample demonstrates C-FIND (querying).
func runFindExample() {
	fmt.Println("=== C-FIND Example (Query) ===")

	// Build a query dataset
	queryDS := dataset.NewDataset()
	queryDS.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY")))
	queryDS.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Smith*")))
	queryDS.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte{}))
	queryDS.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000D), dataelem.UI, []byte{}))

	fmt.Println("Query:")
	fmt.Println("  Level: STUDY")
	fmt.Println("  Patient Name: Smith* (wildcard)")
	fmt.Println("  Requesting: Patient ID, Study Instance UID")
	fmt.Println()
	fmt.Println("To query a real PACS:")
	fmt.Println("  results, _ := scu.Find(ctx, queryDS)")
	fmt.Println("  for result := range results {")
	fmt.Println("      // Process each matching study")
	fmt.Println("      fmt.Println(result.DataSet)")
	fmt.Println("  }")
	_ = queryDS
}

// showHandlerPatterns demonstrates different handler patterns.
func showHandlerPatterns() {
	fmt.Println("=== Handler Patterns ===")
	fmt.Println()

	// Pattern 1: Echo-only handler
	fmt.Println("1. Echo Handler (verification only):")
	fmt.Println("   scp.SetHandler(&network.EchoHandler{})")
	fmt.Println()

	// Pattern 2: Storage handler with callback
	fmt.Println("2. Storage Handler (receive files):")
	fmt.Println("   scp.SetHandler(&network.StorageHandler{")
	fmt.Println("       OnStore: func(ctx context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {")
	fmt.Println("           // Save to disk, database, etc.")
	fmt.Println("           // Works with ALL DICOM types: CT, MR, US, XR, PET, RT, SR, waveforms, etc.")
	fmt.Println("           return network.StatusSuccess")
	fmt.Println("       },")
	fmt.Println("   })")
	fmt.Println()

	// Pattern 3: Query/Retrieve handler
	fmt.Println("3. Query/Retrieve Handler:")
	fmt.Println("   scp.SetHandler(&network.QueryRetrieveHandler{")
	fmt.Println("       OnFind: func(ctx context.Context, sopClass string, query *dataset.Dataset) ([]*dataset.Dataset, error) {")
	fmt.Println("           // Search your database and return matching datasets")
	fmt.Println("           return results, nil")
	fmt.Println("       },")
	fmt.Println("   })")
	fmt.Println()

	// Pattern 4: Composite handler
	fmt.Println("4. Composite Handler (mix & match):")
	fmt.Println("   h := network.NewCompositeHandler()")
	fmt.Println("   h.SetStoreHandler(myStoreHandler)")
	fmt.Println("   h.SetFindHandler(myFindHandler)")
	fmt.Println("   scp.SetHandler(h)")
	fmt.Println()

	// Pattern 5: Custom handler
	fmt.Println("5. Custom Handler (implement interface):")
	fmt.Println("   type MyHandler struct { network.BaseHandler }")
	fmt.Println("   func (h *MyHandler) HandleCStore(ctx, req) (*CStoreResponse, error) { ... }")
	fmt.Println("   func (h *MyHandler) HandleCFind(ctx, req) ([]*CFindResponse, error) { ... }")
	fmt.Println()

	// Supported modalities
	fmt.Println("Supported DICOM Types (all can be sent/received via network):")
	fmt.Printf("  Storage SOP Classes: %d+\n", len(network.AllStorageSOPClassUIDs()))
	fmt.Printf("  Transfer Syntaxes:   %d\n", len(network.AllTransferSyntaxUIDs()))
	fmt.Println("  Modalities: CT, MR, US, XR, CR, DX, MG, PET, NM, RT, SR, VL, XA, XRF")
	fmt.Println("  File formats: .dcm, .ima, .dicom, DICOMDIR, raw DICOM (extensionless)")
	fmt.Println("  Documents: PDF, CDA, STL, OBJ (encapsulated)")
	fmt.Println("  Waveforms: ECG, EEG, EMG, hemodynamic, respiratory")
}
