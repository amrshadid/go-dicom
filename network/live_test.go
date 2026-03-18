package network

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestLiveServerStoreAndVerify starts a real SCP on a real TCP port,
// connects an SCU, sends a CT image dataset, and verifies the SCP received
// the correct SOP Class, SOP Instance, Patient Name, and Patient ID.
func TestLiveServerStoreAndVerify(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Track what the server receives
	var mu sync.Mutex
	type receivedInstance struct {
		SOPClassUID    string
		SOPInstanceUID string
		PatientName    string
		PatientID      string
	}
	var received []receivedInstance

	// Start a real SCP server on a random port, accepting ALL storage SOP classes
	server, err := StartServer(ctx, SCPConfig{
		AETitle:     "LIVE_SCP",
		Port:        0, // random port
		BindAddress: "127.0.0.1",
	}, &StorageHandler{
		OnStore: func(_ context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
			inst := receivedInstance{
				SOPClassUID:    sopClass,
				SOPInstanceUID: sopInstance,
			}
			// Extract patient info from dataset
			if ds != nil {
				if elem, ok := ds.Get(tag.New(0x0010, 0x0010)); ok {
					if v, ok := elem.GetValue().([]byte); ok {
						inst.PatientName = trimNulls(string(v))
					}
				}
				if elem, ok := ds.Get(tag.New(0x0010, 0x0020)); ok {
					if v, ok := elem.GetValue().([]byte); ok {
						inst.PatientID = trimNulls(string(v))
					}
				}
			}
			mu.Lock()
			received = append(received, inst)
			mu.Unlock()
			return StatusSuccess
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	// Accept ALL storage SOP classes + verification
	allSyntaxes := append([]string{VerificationSOPClassUID}, AllStorageSOPClassUIDs()...)
	server.SetSupportedAbstractSyntaxes(allSyntaxes)

	addr := server.Addr()
	t.Logf("SCP listening on %s", addr)

	// === Send a CT Image ===
	ctDataset := buildTestCTDataset("1.2.3.4.5.6.7.8.1", "Smith^John", "CT-001")
	sendAndVerify(t, ctx, addr, "LIVE_SCP", ctDataset)

	// === Send an MR Image ===
	mrDataset := buildTestMRDataset("1.2.3.4.5.6.7.8.2", "Doe^Jane", "MR-002")
	sendAndVerify(t, ctx, addr, "LIVE_SCP", mrDataset)

	// === Send an Ultrasound Image ===
	usDataset := buildTestUSDataset("1.2.3.4.5.6.7.8.3", "Garcia^Maria", "US-003")
	sendAndVerify(t, ctx, addr, "LIVE_SCP", usDataset)

	// === Send a Secondary Capture ===
	scDataset := buildTestSCDataset("1.2.3.4.5.6.7.8.4", "Chen^Wei", "SC-004")
	sendAndVerify(t, ctx, addr, "LIVE_SCP", scDataset)

	// === Send an RT Plan ===
	rtDataset := buildTestRTDataset("1.2.3.4.5.6.7.8.5", "Brown^Alice", "RT-005")
	sendAndVerify(t, ctx, addr, "LIVE_SCP", rtDataset)

	// Verify all 5 were received
	mu.Lock()
	defer mu.Unlock()

	if len(received) != 5 {
		t.Fatalf("expected 5 received instances, got %d", len(received))
	}

	// Verify each instance
	checks := []struct {
		sopClass    string
		sopInstance string
		patient     string
		patientID   string
	}{
		{CTImageStorageUID, "1.2.3.4.5.6.7.8.1", "Smith^John", "CT-001"},
		{MRImageStorageUID, "1.2.3.4.5.6.7.8.2", "Doe^Jane", "MR-002"},
		{USImageStorageUID, "1.2.3.4.5.6.7.8.3", "Garcia^Maria", "US-003"},
		{SecondaryCaptureImageStorageUID, "1.2.3.4.5.6.7.8.4", "Chen^Wei", "SC-004"},
		{RTPlanStorageUID, "1.2.3.4.5.6.7.8.5", "Brown^Alice", "RT-005"},
	}

	for i, check := range checks {
		r := received[i]
		if r.SOPClassUID != check.sopClass {
			t.Errorf("instance %d: SOP Class = %s, want %s", i, r.SOPClassUID, check.sopClass)
		}
		if r.SOPInstanceUID != check.sopInstance {
			t.Errorf("instance %d: SOP Instance = %s, want %s", i, r.SOPInstanceUID, check.sopInstance)
		}
		if r.PatientName != check.patient {
			t.Errorf("instance %d: Patient = %q, want %q", i, r.PatientName, check.patient)
		}
		if r.PatientID != check.patientID {
			t.Errorf("instance %d: Patient ID = %q, want %q", i, r.PatientID, check.patientID)
		}
		t.Logf("  [%d] %s %s — %s (%s)", i+1, r.SOPClassUID, r.SOPInstanceUID, r.PatientName, r.PatientID)
	}
}

// TestLiveServerEchoMultipleClients tests 10 concurrent SCU clients pinging one SCP.
func TestLiveServerEchoMultipleClients(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle:     "ECHO_LIVE",
		Port:        0,
		BindAddress: "127.0.0.1",
	}, &EchoHandler{})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	addr := server.Addr()
	numClients := 10
	var wg sync.WaitGroup
	var successCount int32
	var mu sync.Mutex

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			scu := NewSCU(SCUConfig{
				CallingAE: fmt.Sprintf("CLIENT_%d", id),
				CalledAE:  "ECHO_LIVE",
				Address:   addr,
			})

			if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
				t.Errorf("client %d Associate: %v", id, err)
				return
			}

			if err := scu.Echo(ctx); err != nil {
				t.Errorf("client %d Echo: %v", id, err)
				scu.Abort(ctx)
				return
			}

			scu.Release(ctx)
			mu.Lock()
			successCount++
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	mu.Lock()
	if int(successCount) != numClients {
		t.Errorf("expected %d successful echoes, got %d", numClients, successCount)
	}
	mu.Unlock()

	t.Logf("All %d concurrent clients pinged successfully", numClients)
}

// TestLiveServerGroupThreeServers runs 3 separate servers and echoes each.
func TestLiveServerGroupThreeServers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	group := NewServerGroup()

	s1, err := group.Add(ctx, SCPConfig{AETitle: "ECHO_1", Port: 0, BindAddress: "127.0.0.1"}, &EchoHandler{})
	if err != nil {
		t.Fatalf("server 1: %v", err)
	}
	s2, err := group.Add(ctx, SCPConfig{AETitle: "STORE_2", Port: 0, BindAddress: "127.0.0.1"}, &BaseHandler{})
	if err != nil {
		t.Fatalf("server 2: %v", err)
	}
	s3, err := group.Add(ctx, SCPConfig{AETitle: "QR_3", Port: 0, BindAddress: "127.0.0.1"}, &BaseHandler{})
	if err != nil {
		t.Fatalf("server 3: %v", err)
	}

	t.Logf("Server 1: %s on %s", "ECHO_1", s1.Addr())
	t.Logf("Server 2: %s on %s", "STORE_2", s2.Addr())
	t.Logf("Server 3: %s on %s", "QR_3", s3.Addr())

	// Echo all 3
	for i, srv := range []*Server{s1, s2, s3} {
		aet := []string{"ECHO_1", "STORE_2", "QR_3"}[i]
		scu := NewSCU(SCUConfig{
			CallingAE: "GROUP_TEST",
			CalledAE:  aet,
			Address:   srv.Addr(),
		})
		if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
			t.Fatalf("server %d associate: %v", i+1, err)
		}
		if err := scu.Echo(ctx); err != nil {
			t.Fatalf("server %d echo: %v", i+1, err)
		}
		scu.Release(ctx)
		t.Logf("  Server %d (%s): C-ECHO OK", i+1, aet)
	}

	group.StopAll()
}

// --- Helper functions ---

func sendAndVerify(t *testing.T, ctx context.Context, addr, calledAE string, ds *dataset.Dataset) {
	t.Helper()

	// Extract SOP Class UID to build the right presentation context
	sopClassElem, ok := ds.Get(tag.New(0x0008, 0x0016))
	if !ok {
		t.Fatal("dataset missing SOP Class UID")
	}
	sopClassUID := trimNulls(string(sopClassElem.GetValue().([]byte)))

	// Propose the specific SOP class + verification
	contexts := []PresentationContextItem{
		{ID: 1, AbstractSyntax: VerificationSOPClassUID, TransferSyntaxes: DefaultTransferSyntaxes()},
		{ID: 3, AbstractSyntax: sopClassUID, TransferSyntaxes: DefaultTransferSyntaxes()},
	}

	scu := NewSCU(SCUConfig{
		CallingAE: "LIVE_SCU",
		CalledAE:  calledAE,
		Address:   addr,
	})

	if err := scu.Associate(ctx, contexts); err != nil {
		t.Fatalf("Associate: %v", err)
	}

	if err := scu.Store(ctx, ds); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if err := scu.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func buildTestCTDataset(sopInstanceUID, patientName, patientID string) *dataset.Dataset {
	ds := dataset.NewDataset()
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(CTImageStorageUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte(sopInstanceUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("CT")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte(patientName)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte(patientID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000D), dataelem.UI, []byte("1.2.3.4.5.6.100")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000E), dataelem.UI, []byte("1.2.3.4.5.6.101")))
	return ds
}

func buildTestMRDataset(sopInstanceUID, patientName, patientID string) *dataset.Dataset {
	ds := dataset.NewDataset()
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(MRImageStorageUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte(sopInstanceUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("MR")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte(patientName)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte(patientID)))
	return ds
}

func buildTestUSDataset(sopInstanceUID, patientName, patientID string) *dataset.Dataset {
	ds := dataset.NewDataset()
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(USImageStorageUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte(sopInstanceUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("US")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte(patientName)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte(patientID)))
	return ds
}

func buildTestSCDataset(sopInstanceUID, patientName, patientID string) *dataset.Dataset {
	ds := dataset.NewDataset()
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(SecondaryCaptureImageStorageUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte(sopInstanceUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("OT")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte(patientName)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte(patientID)))
	return ds
}

func buildTestRTDataset(sopInstanceUID, patientName, patientID string) *dataset.Dataset {
	ds := dataset.NewDataset()
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(RTPlanStorageUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte(sopInstanceUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("RTPLAN")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte(patientName)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte(patientID)))
	return ds
}

func trimNulls(s string) string {
	for len(s) > 0 && (s[len(s)-1] == 0 || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
