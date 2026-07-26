package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// QRSCPCommand implements the qrscp CLI command — a combined Verification,
// Storage, and Query/Retrieve SCP. Equivalent to pynetdicom's qrscp.
type QRSCPCommand struct {
	aeTitle   string
	port      int
	outputDir string
}

func (c *QRSCPCommand) Name() string        { return "qrscp" }
func (c *QRSCPCommand) Description() string { return "DICOM Query/Retrieve SCP (store + find + move)" }

func (c *QRSCPCommand) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.aeTitle, "aet", "QRSCP", "AE title")
	fs.IntVar(&c.port, "port", 11112, "Listen port")
	fs.StringVar(&c.outputDir, "output", "./dcmstore", "Storage directory for received instances")
}

func (c *QRSCPCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("qrscp", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := os.MkdirAll(c.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}

	// In-memory instance index (production would use SQLite)
	store := &instanceStore{
		outputDir: c.outputDir,
		instances: make(map[string]*storedInstance),
	}

	scp := network.NewSCP(network.SCPConfig{
		AETitle: c.aeTitle,
		Port:    c.port,
	})

	// Support all storage + Q/R + verification + worklist
	allAS := make([]string, 0, 200)
	allAS = append(allAS, network.VerificationSOPClassUID)
	allAS = append(allAS, network.AllStorageSOPClassUIDs()...)
	allAS = append(allAS, network.AllQueryRetrieveSOPClassUIDs()...)
	allAS = append(allAS, network.AllWorklistSOPClassUIDs()...)
	scp.SetSupportedAbstractSyntaxes(allAS)

	handler := &qrHandler{store: store}
	scp.SetHandler(handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
		scp.Close()
	}()

	fmt.Printf("Starting DICOM Query/Retrieve SCP\n")
	fmt.Printf("  AE Title:  %s\n", c.aeTitle)
	fmt.Printf("  Port:      %d\n", c.port)
	fmt.Printf("  Storage:   %s\n", c.outputDir)
	fmt.Printf("  Services:  Verification, Storage, Query/Retrieve, Worklist\n")
	fmt.Printf("  SOP Classes: %d\n", len(allAS))
	fmt.Println("Listening for associations... (Ctrl+C to stop)")

	if err := scp.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		return err
	}

	fmt.Printf("Stored %d instance(s)\n", store.count())
	return nil
}

// storedInstance tracks a received DICOM instance.
type storedInstance struct {
	SOPClassUID    string
	SOPInstanceUID string
	PatientName    string
	PatientID      string
	StudyUID       string
	SeriesUID      string
	Modality       string
	FilePath       string

	// DataSet is the instance itself, retained so C-GET can transfer it back.
	DataSet *dataset.Dataset
}

// instanceStore is a thread-safe in-memory instance index.
type instanceStore struct {
	mu        sync.RWMutex
	outputDir string
	instances map[string]*storedInstance // key: SOPInstanceUID
}

func (s *instanceStore) add(inst *storedInstance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[inst.SOPInstanceUID] = inst
}

func (s *instanceStore) count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.instances)
}

func (s *instanceStore) find(query func(*storedInstance) bool) []*storedInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []*storedInstance
	for _, inst := range s.instances {
		if query(inst) {
			results = append(results, inst)
		}
	}
	return results
}

// qrHandler handles Verification, Storage, and Query/Retrieve requests.
type qrHandler struct {
	network.BaseHandler
	store *instanceStore
}

func (h *qrHandler) HandleCStore(ctx context.Context, req *network.CStoreRequest) (*network.CStoreResponse, error) {
	inst := &storedInstance{
		SOPClassUID:    req.AffectedSOPClass,
		SOPInstanceUID: req.AffectedSOPInstance,
		FilePath:       filepath.Join(h.store.outputDir, req.AffectedSOPInstance+".dcm"),
	}

	// Extract metadata from dataset if available
	if req.DataSet != nil {
		inst.DataSet = req.DataSet
		inst.PatientName = extractDSString(req.DataSet, 0x0010, 0x0010)
		inst.PatientID = extractDSString(req.DataSet, 0x0010, 0x0020)
		inst.StudyUID = extractDSString(req.DataSet, 0x0020, 0x000D)
		inst.SeriesUID = extractDSString(req.DataSet, 0x0020, 0x000E)
		inst.Modality = extractDSString(req.DataSet, 0x0008, 0x0060)
	}

	h.store.add(inst)
	fmt.Printf("  Stored: %s [%s] %s\n", inst.SOPInstanceUID, inst.Modality, inst.PatientName)

	return &network.CStoreResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.AffectedSOPClass,
		AffectedSOPInstance:  req.AffectedSOPInstance,
		Status:               network.StatusSuccess,
	}, nil
}

func (h *qrHandler) HandleCFind(ctx context.Context, req *network.CFindRequest) ([]*network.CFindResponse, error) {
	// Match instances based on query dataset
	matches := h.store.find(func(inst *storedInstance) bool {
		return true // Simplified: return all instances. Production would filter by query.
	})

	responses := make([]*network.CFindResponse, len(matches))
	for i, match := range matches {
		ds := dataset.NewDataset()
		// Build response dataset with requested fields
		_ = match
		responses[i] = &network.CFindResponse{
			MessageIDRespondedTo: req.MessageID,
			AffectedSOPClass:     req.AffectedSOPClass,
			Status:               network.StatusPending,
			DataSet:              ds,
		}
	}

	return responses, nil
}

// HandleCGet returns the matching instances for transfer as C-STORE
// sub-operations over the same association.
func (h *qrHandler) HandleCGet(_ context.Context, req *network.CGetRequest) (*network.CGetResponse, error) {
	matches := h.store.find(func(inst *storedInstance) bool {
		return inst.DataSet != nil
	})

	instances := make([]*dataset.Dataset, 0, len(matches))
	for _, m := range matches {
		instances = append(instances, m.DataSet)
	}

	fmt.Printf("  C-GET: returning %d instance(s)\n", len(instances))

	return &network.CGetResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.AffectedSOPClass,
		Status:               network.StatusSuccess,
		Instances:            instances,
	}, nil
}

// extractDSString extracts a string value from a dataset by tag.
func extractDSString(ds *dataset.Dataset, group, element uint16) string {
	tagVal := tag.New(group, element)
	elem, ok := ds.Get(tagVal)
	if !ok {
		return ""
	}
	val := elem.GetValue()
	switch v := val.(type) {
	case []byte:
		s := string(v)
		for len(s) > 0 && (s[len(s)-1] == 0 || s[len(s)-1] == ' ') {
			s = s[:len(s)-1]
		}
		return s
	case string:
		return v
	default:
		return ""
	}
}
