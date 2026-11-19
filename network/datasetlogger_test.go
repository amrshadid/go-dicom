package network

import (
	"context"
	"encoding/binary"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// The data sets these tests build for themselves carry retired attributes on
// purpose, so building them warns; quiet is where those warnings go, so that
// only what the network decodes reaches the loggers under test.
var quiet = config.WithLogger(context.Background(), slog.New(slog.DiscardHandler))

// Retired attributes (PS3.6 lists them as retired), one for each direction a
// data set travels in these tests, so that a line in a logger says which
// decode wrote it.
var (
	tagQueryOnly    = tag.New(0x0040, 0xA16A) // Bibliographic Citation (Trial), in a C-FIND identifier sent
	tagFindResult   = tag.New(0x0008, 0x0010) // Recognition Code, in a C-FIND result returned
	tagNRequest     = tag.New(0x0008, 0x1000) // Network ID, in an N-service request data set
	tagNResponse    = tag.New(0x0020, 0x3401) // Modifying Device ID, in an N-service reply
	tagLengthToEnd  = tag.New(0x0000, 0x0001) // Command Length to End, in a command set
	semanticWarning = "dataset: semantic validation"
)

// withRetired is a data set of the given elements, built quietly.
func withRetired(elems ...*dataelem.DataElement) *dataset.Dataset {
	ds := dataset.NewDataset()
	for _, e := range elems {
		_ = ds.AddWithContext(quiet, e)
	}
	return ds
}

// retiredHandler keeps what is stored, and returns it to a C-GET and to a
// C-MOVE. Its C-FIND results and N-service replies carry a retired attribute.
type retiredHandler struct {
	BaseHandler
	mu     sync.Mutex
	stores int
	stored map[string]*dataset.Dataset
}

func (h *retiredHandler) HandleCStore(_ context.Context, req *CStoreRequest) (*CStoreResponse, error) {
	h.mu.Lock()
	if h.stored == nil {
		h.stored = map[string]*dataset.Dataset{}
	}
	h.stored[req.AffectedSOPInstance] = req.DataSet
	h.stores++
	h.mu.Unlock()
	return &CStoreResponse{MessageIDRespondedTo: req.MessageID, AffectedSOPClass: req.AffectedSOPClass,
		AffectedSOPInstance: req.AffectedSOPInstance, Status: StatusSuccess}, nil
}

// storeCount is the number of C-STOREs received.
func (h *retiredHandler) storeCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stores
}

// instances is one data set of each SOP Instance UID stored, in UID order.
func (h *retiredHandler) instances() []*dataset.Dataset {
	h.mu.Lock()
	defer h.mu.Unlock()
	keys := make([]string, 0, len(h.stored))
	for k := range h.stored {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*dataset.Dataset, 0, len(keys))
	for _, k := range keys {
		out = append(out, h.stored[k])
	}
	return out
}

func (h *retiredHandler) HandleCFind(_ context.Context, req *CFindRequest) ([]*CFindResponse, error) {
	return []*CFindResponse{
		{MessageIDRespondedTo: req.MessageID, AffectedSOPClass: req.AffectedSOPClass, Status: StatusPending,
			DataSet: withRetired(
				dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY ")),
				dataelem.NewDataElement(tagFindResult, dataelem.SH, []byte("ACR-NEMA"))),
		},
	}, nil
}

func (h *retiredHandler) HandleCGet(_ context.Context, req *CGetRequest) (*CGetResponse, error) {
	return &CGetResponse{MessageIDRespondedTo: req.MessageID, AffectedSOPClass: req.AffectedSOPClass,
		Status: StatusSuccess, Instances: h.instances()}, nil
}

func (h *retiredHandler) HandleCMove(_ context.Context, req *CMoveRequest) (*CMoveResponse, error) {
	return &CMoveResponse{MessageIDRespondedTo: req.MessageID, AffectedSOPClass: req.AffectedSOPClass,
		Status: StatusSuccess, Instances: h.instances()}, nil
}

// nReply is the attribute list every N-service reply carries.
func nReply() *dataset.Dataset {
	return withRetired(dataelem.NewDataElement(tagNResponse, dataelem.CS, []byte("DEVICE")))
}

func (h *retiredHandler) HandleNCreate(_ context.Context, req *NCreateRequest) (*NCreateResponse, error) {
	return &NCreateResponse{MessageIDRespondedTo: req.MessageID, AffectedSOPClass: req.AffectedSOPClass,
		AffectedSOPInstance: req.AffectedSOPInstance, Status: StatusSuccess, DataSet: nReply()}, nil
}

func (h *retiredHandler) HandleNSet(_ context.Context, req *NSetRequest) (*NSetResponse, error) {
	return &NSetResponse{MessageIDRespondedTo: req.MessageID, AffectedSOPClass: req.RequestedSOPClass,
		AffectedSOPInstance: req.RequestedSOPInstance, Status: StatusSuccess, DataSet: nReply()}, nil
}

func (h *retiredHandler) HandleNGet(_ context.Context, req *NGetRequest) (*NGetResponse, error) {
	return &NGetResponse{MessageIDRespondedTo: req.MessageID, AffectedSOPClass: req.RequestedSOPClass,
		AffectedSOPInstance: req.RequestedSOPInstance, Status: StatusSuccess, DataSet: nReply()}, nil
}

func (h *retiredHandler) HandleNAction(_ context.Context, req *NActionRequest) (*NActionResponse, error) {
	return &NActionResponse{MessageIDRespondedTo: req.MessageID, AffectedSOPClass: req.RequestedSOPClass,
		AffectedSOPInstance: req.RequestedSOPInstance, ActionTypeID: req.ActionTypeID, Status: StatusSuccess,
		DataSet: nReply()}, nil
}

func (h *retiredHandler) HandleNEventReport(_ context.Context, req *NEventReportRequest) (*NEventReportResponse, error) {
	return &NEventReportResponse{MessageIDRespondedTo: req.MessageID, AffectedSOPClass: req.AffectedSOPClass,
		AffectedSOPInstance: req.AffectedSOPInstance, EventTypeID: req.EventTypeID, Status: StatusSuccess,
		DataSet: nReply()}, nil
}

// corpusInstances is every file of the pydicom corpus that reads as an
// instance with a SOP Class and a SOP Instance UID, read with a logger that
// discards: what the read itself logs is filereader's business (#481).
func corpusInstances(t *testing.T) []*dataset.Dataset {
	t.Helper()
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		if os.Getenv("GODICOM_REQUIRE_INTEROP") != "" {
			t.Fatal("GODICOM_REQUIRE_INTEROP is set but GODICOM_PYDICOM_DATA is not")
		}
		t.Skip("GODICOM_PYDICOM_DATA is not set; skipping the corpus-backed logging check")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.dcm"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no corpus files in %s: %v", dir, err)
	}
	var out []*dataset.Dataset
	for _, name := range names {
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(f),
			filereader.ReadOptions{Logger: slog.New(slog.DiscardHandler)})
		_ = f.Close()
		if err != nil {
			continue
		}
		ds := df.GetDataset()
		if _, ok := ds.Get(tag.New(0x0008, 0x0016)); !ok {
			continue
		}
		if _, ok := ds.Get(tag.New(0x0008, 0x0018)); !ok {
			continue
		}
		out = append(out, ds)
	}
	return out
}

// corpusContexts proposes each SOP Class of instances once, with the
// syntaxes they are in and the uncompressed ones, after first.
func corpusContexts(instances []*dataset.Dataset, first []PresentationContextItem) ([]PresentationContextItem, []string) {
	syntaxes := map[string][]string{}
	var classes []string
	for _, ds := range instances {
		elem, _ := ds.Get(tag.New(0x0008, 0x0016))
		class := extractStringValue(elem)
		if _, ok := syntaxes[class]; !ok {
			classes = append(classes, class)
			syntaxes[class] = DefaultTransferSyntaxes()
		}
		ts := ds.TransferSyntaxUID()
		if ts == "" {
			continue
		}
		found := false
		for _, s := range syntaxes[class] {
			found = found || s == ts
		}
		if !found {
			syntaxes[class] = append([]string{ts}, syntaxes[class]...)
		}
	}
	contexts := append([]PresentationContextItem(nil), first...)
	for _, class := range classes {
		contexts = append(contexts, PresentationContextItem{
			ID: byte(2*len(contexts) + 1), AbstractSyntax: class, TransferSyntaxes: syntaxes[class]})
	}
	return contexts, classes
}

// semanticLines is the semantic validation lines of a log.
func semanticLines(log string) int {
	return strings.Count(log, semanticWarning)
}

// TestDataSetWarningsGoToTheServersAndClientsOwnLogger: a server and a client
// given their own Logger, and the destination of a C-MOVE given another,
// exchange every corpus file that can be sent as a C-STORE, a C-FIND, a C-GET
// and a C-MOVE of all of them, the N-services with a data set each way, a
// storage commitment, and a command set carrying a retired element. Both
// DefaultLogger and config.Logger fail the test on any write. The validation
// warnings that dataset.Add raises while the network decodes each data set
// are in the logger of the side that decoded it: what the server received in
// the server's, what the client received in the client's, what the C-MOVE
// delivered in the destination's. They all went to config.Logger, whatever
// logger the server or client had been given (#503).
func TestDataSetWarningsGoToTheServersAndClientsOwnLogger(t *testing.T) {
	instances := corpusInstances(t)
	noGlobalLog(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	serverLog, serverBuf := ownLog()
	destLog, destBuf := ownLog()
	clientLog, clientBuf := ownLog()

	qr := []PresentationContextItem{
		{ID: 1, AbstractSyntax: VerificationSOPClassUID, TransferSyntaxes: DefaultTransferSyntaxes()},
		{ID: 3, AbstractSyntax: StudyRootQueryRetrieveFind, TransferSyntaxes: DefaultTransferSyntaxes()},
		{ID: 5, AbstractSyntax: StudyRootQueryRetrieveMove, TransferSyntaxes: DefaultTransferSyntaxes()},
		{ID: 7, AbstractSyntax: StudyRootQueryRetrieveGet, TransferSyntaxes: DefaultTransferSyntaxes()},
		{ID: 9, AbstractSyntax: ModalityPerformedProcedureStepUID, TransferSyntaxes: DefaultTransferSyntaxes()},
	}
	contexts, classes := corpusContexts(instances, qr)
	abstract := append([]string{VerificationSOPClassUID, StudyRootQueryRetrieveFind, StudyRootQueryRetrieveMove,
		StudyRootQueryRetrieveGet, ModalityPerformedProcedureStepUID}, classes...)
	allSyntaxes := map[string]bool{}
	for _, c := range contexts {
		for _, ts := range c.TransferSyntaxes {
			allSyntaxes[ts] = true
		}
	}
	syntaxes := make([]string, 0, len(allSyntaxes))
	for ts := range allSyntaxes {
		syntaxes = append(syntaxes, ts)
	}
	sort.Strings(syntaxes)

	destHandler := &retiredHandler{}
	dest, err := StartServer(ctx, SCPConfig{AETitle: "DEST", Port: 0, BindAddress: "127.0.0.1", Logger: destLog,
		AbstractSyntaxes: abstract, TransferSyntaxes: syntaxes}, destHandler)
	if err != nil {
		t.Fatal(err)
	}
	defer dest.Stop()

	handler := &retiredHandler{}
	server, err := StartServer(ctx, SCPConfig{AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1", Logger: serverLog,
		AbstractSyntaxes: abstract, TransferSyntaxes: syntaxes,
		MoveDestinations: map[string]string{"DEST": dest.Addr()}}, handler)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	roles := make([]SCPSCURoleSelection, 0, len(classes))
	for _, class := range classes {
		roles = append(roles, SCPSCURoleSelection{SOPClassUID: class, SCURole: true, SCPRole: true})
	}
	var received sync.Map
	scu := NewSCU(SCUConfig{CallingAE: "MODALITY", CalledAE: "ARCHIVE", Address: server.Addr(), Logger: clientLog,
		ExtendedNegotiation: &ExtendedNegotiation{RoleSelections: roles},
		OnCStore: func(_ context.Context, _, sopInstance string, _ *dataset.Dataset) uint16 {
			received.Store(sopInstance, true)
			return StatusSuccess
		}})
	if err := scu.Associate(ctx, contexts); err != nil {
		t.Fatal(err)
	}

	// C-STORE every instance there is a context for.
	sent := 0
	for _, ds := range instances {
		if err := scu.Store(ctx, ds); err == nil {
			sent++
		}
	}
	stored, distinct := handler.storeCount(), len(handler.instances())
	t.Logf("%d corpus files read, %d sent, %d stored, %d distinct SOP Instance UIDs", len(instances), sent, stored, distinct)
	if stored < 60 || stored != sent {
		t.Fatalf("%d of %d corpus instances sent were stored", stored, sent)
	}
	afterStore := semanticLines(serverBuf.String())
	if afterStore == 0 {
		t.Fatalf("the server's log has no %q line after %d corpus instances:\n%s", semanticWarning, stored, serverBuf.String())
	}
	eventually(t, "the server", serverBuf, "(0010,1000)")

	// The compressed ones again, to DEST, from a client that proposes only
	// the uncompressed syntaxes, so that each is decoded before it is sent:
	// encoding builds data sets too. To DEST, so that what ARCHIVE returns
	// below is what was sent to it as it was.
	uncompressed := make([]PresentationContextItem, len(contexts))
	for i, c := range contexts {
		c.TransferSyntaxes = DefaultTransferSyntaxes()
		uncompressed[i] = c
	}
	plain := NewSCU(SCUConfig{CallingAE: "PLAIN", CalledAE: "DEST", Address: dest.Addr(), Logger: clientLog})
	if err := plain.Associate(ctx, uncompressed); err != nil {
		t.Fatal(err)
	}
	transcoded := 0
	for _, ds := range instances {
		ts := ds.TransferSyntaxUID()
		if ts == ExplicitVRLittleEndianUID || ts == ImplicitVRLittleEndianUID || ts == ExplicitVRBigEndianUID ||
			ts == DeflatedExplicitVRLittleEndianUID {
			continue
		}
		if err := plain.Store(ctx, ds); err == nil {
			transcoded++
		}
	}
	t.Logf("%d compressed instances decoded and sent uncompressed", transcoded)
	if transcoded == 0 {
		t.Fatal("no compressed instance was sent over an uncompressed context")
	}
	if err := plain.Release(ctx); err != nil {
		t.Fatal(err)
	}

	// C-FIND: the identifier sent and the result returned.
	results, err := scu.Find(ctx, withRetired(
		dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY ")),
		dataelem.NewDataElement(tagQueryOnly, dataelem.ST, []byte("CITATION"))))
	if err != nil {
		t.Fatal(err)
	}
	for r := range results {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
	}

	// C-GET: every stored instance comes back to the client.
	query := withRetired(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY ")))
	if err := scu.Get(ctx, query); err != nil {
		t.Logf("C-GET: %v", err)
	}
	got := 0
	received.Range(func(any, any) bool { got++; return true })
	t.Logf("C-GET: %d of %d instances received", got, distinct)
	if got < distinct*3/4 {
		t.Fatalf("the client received %d of %d instances from the C-GET", got, distinct)
	}

	// C-MOVE: every stored instance goes to DEST.
	if err := scu.Move(ctx, query, "DEST"); err != nil {
		t.Logf("C-MOVE: %v", err)
	}
	moved := destHandler.storeCount() - transcoded
	t.Logf("C-MOVE: %d of %d instances received", moved, distinct)
	if moved < distinct*3/4 {
		t.Fatalf("DEST received %d of %d instances from the C-MOVE", moved, distinct)
	}

	// The N-services, a data set each way.
	request := withRetired(dataelem.NewDataElement(tagNRequest, dataelem.AE, []byte("NETWORK ")))
	const instance = "1.2.826.0.1.3680043.2.1125.503"
	if _, err := scu.NCreate(ctx, ModalityPerformedProcedureStepUID, instance, request); err != nil {
		t.Fatal(err)
	}
	if _, err := scu.NSet(ctx, ModalityPerformedProcedureStepUID, instance, request); err != nil {
		t.Fatal(err)
	}
	if _, err := scu.NGet(ctx, ModalityPerformedProcedureStepUID, instance); err != nil {
		t.Fatal(err)
	}
	if _, err := scu.NAction(ctx, ModalityPerformedProcedureStepUID, instance, 1, request); err != nil {
		t.Fatal(err)
	}
	if _, err := scu.NEventReport(ctx, ModalityPerformedProcedureStepUID, instance, 1, request); err != nil {
		t.Fatal(err)
	}

	// A command set with a retired element, which a peer of an older
	// standard sends.
	assoc := scu.Association()
	messageID := scu.nextMessageID()
	echo := BuildCEchoRQ(messageID)
	length := make([]byte, 4)
	binary.LittleEndian.PutUint32(length, 0)
	_ = echo.AddWithContext(quiet, dataelem.NewDataElement(tagLengthToEnd, dataelem.UL, length))
	echoBytes, err := EncodeCommandDataset(echo)
	if err != nil {
		t.Fatal(err)
	}
	if err := scu.sendMessage(ctx, assoc, 1, echoBytes, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := assoc.ReceiveResponse(ctx, messageID); err != nil {
		t.Fatal(err)
	}
	if err := scu.Release(ctx); err != nil {
		t.Fatal(err)
	}

	commitment(t, ctx)

	eventually(t, "the server", serverBuf, tagQueryOnly.String(), tagNRequest.String(), tagLengthToEnd.String())
	eventually(t, "the client", clientBuf, tagFindResult.String(), tagNResponse.String(), "(0010,1000)")
	eventually(t, "DEST", destBuf, "(0010,1000)")
	for name, c := range map[string]struct {
		log string
		not []string
	}{
		"the server": {serverBuf.String(), []string{tagFindResult.String(), tagNResponse.String()}},
		"the client": {clientBuf.String(), []string{tagQueryOnly.String(), tagNRequest.String(), tagLengthToEnd.String()}},
		"DEST":       {destBuf.String(), []string{tagQueryOnly.String(), tagNRequest.String(), tagFindResult.String(), tagNResponse.String()}},
	} {
		for _, other := range c.not {
			if strings.Contains(c.log, other) {
				t.Errorf("%s's log has %s, which another decoded:\n%s", name, other, c.log)
			}
		}
	}
	t.Logf("validation lines: server %d (%d from the C-STOREs), client %d, DEST %d",
		semanticLines(serverBuf.String()), afterStore, semanticLines(clientBuf.String()), semanticLines(destBuf.String()))
}

// commitment runs a storage commitment, request and report on one
// association, between a server and a client each given their own logger.
func commitment(t *testing.T, ctx context.Context) {
	t.Helper()
	serverLog, _ := ownLog()
	clientLog, _ := ownLog()
	handler := &StorageCommitmentHandler{
		OnCommit: func(_ context.Context, req *StorageCommitmentRequest) (*StorageCommitmentResult, error) {
			return &StorageCommitmentResult{Successful: req.Instances}, nil
		},
	}
	server, err := StartServer(ctx, SCPConfig{AETitle: "COMMIT_SCP", Port: 0, BindAddress: "127.0.0.1", Logger: serverLog}, handler)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	scu := NewSCU(SCUConfig{CallingAE: "COMMIT_SCU", CalledAE: "COMMIT_SCP", Address: server.Addr(), Logger: clientLog})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scu.Release(ctx) }()
	if _, err := scu.RequestStorageCommitment(ctx, &StorageCommitmentRequest{
		TransactionUID: "1.2.826.0.1.3680043.2.1125.503.1",
		Instances:      instanceRefs("1.2.3.1", "1.2.3.2"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := scu.ReceiveStorageCommitmentResult(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestDataSetWarningsWithoutALoggerGoToTheGlobalOne: a server and a client
// given no Logger write the validation warnings of what they decode to
// config.Logger, as they always did.
func TestDataSetWarningsWithoutALoggerGoToTheGlobalOne(t *testing.T) {
	global := &logBuffer{}
	previous := config.Logger
	config.Logger = slog.New(slog.NewTextHandler(global, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { config.Logger = previous })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	handler := &retiredHandler{}
	server, err := StartServer(ctx, SCPConfig{AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1"}, handler)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	scu := NewSCU(SCUConfig{CallingAE: "MODALITY", CalledAE: "ARCHIVE", Address: server.Addr()})
	if err := scu.Associate(ctx, []PresentationContextItem{
		{ID: 1, AbstractSyntax: StudyRootQueryRetrieveFind, TransferSyntaxes: DefaultTransferSyntaxes()},
		{ID: 3, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: DefaultTransferSyntaxes()},
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scu.Release(ctx) }()

	ct := withRetired(
		dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(CTImageStorageUID)),
		dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.826.0.1.3680043.2.1125.503.2\x00")),
		dataelem.NewDataElement(tagNRequest, dataelem.AE, []byte("NETWORK ")))
	if err := scu.Store(ctx, ct); err != nil {
		t.Fatal(err)
	}
	results, err := scu.Find(ctx, withRetired(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY "))))
	if err != nil {
		t.Fatal(err)
	}
	for r := range results {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	eventually(t, "config.Logger", global, semanticWarning, tagNRequest.String(), tagFindResult.String())
}

// upsSteps is a UPSStore over a map.
type upsSteps struct {
	mu    sync.Mutex
	steps map[string]*UPSStep
}

func (s *upsSteps) FindUPS(_ context.Context, uid string) (*UPSStep, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	step, ok := s.steps[uid]
	return step, ok, nil
}

func (s *upsSteps) CreateUPS(_ context.Context, step *UPSStep) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps[step.SOPInstanceUID] = step
	return nil
}

func (s *upsSteps) UpdateUPS(ctx context.Context, step *UPSStep) error {
	return s.CreateUPS(ctx, step)
}

// TestTheUPSHandlerWarnsToTheServersLogger: the built-in UPS handler applies
// the attributes of an N-SET to the step it keeps, element by element, and a
// retired one warns as it is added. That happens in the handler, after the
// network decoded the data set, so the server's logger reaches it through
// the context the handler is given. Both global loggers fail the test on any
// write.
func TestTheUPSHandlerWarnsToTheServersLogger(t *testing.T) {
	noGlobalLog(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	serverLog, serverBuf := ownLog()
	clientLog, _ := ownLog()

	server, err := StartServer(ctx, SCPConfig{AETitle: "WORKLIST", Port: 0, BindAddress: "127.0.0.1", Logger: serverLog,
		AbstractSyntaxes: []string{UnifiedProcedureStepPushUID}},
		&UPSHandler{Store: &upsSteps{steps: map[string]*UPSStep{}}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	scu := NewSCU(SCUConfig{CallingAE: "PERFORMER", CalledAE: "WORKLIST", Address: server.Addr(), Logger: clientLog})
	if err := scu.Associate(ctx, []PresentationContextItem{
		{ID: 1, AbstractSyntax: UnifiedProcedureStepPushUID, TransferSyntaxes: DefaultTransferSyntaxes()},
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scu.Release(ctx) }()

	const step = "1.2.826.0.1.3680043.2.1125.503.3"
	const txn = "1.2.826.0.1.3680043.2.1125.503.4"
	state := tag.New(0x0074, 0x1000)
	txnTag := tag.New(0x0008, 0x1195)
	created, err := scu.NCreate(ctx, UnifiedProcedureStepPushUID, step, withRetired(
		dataelem.NewDataElement(state, dataelem.CS, []byte("SCHEDULED "))))
	if err != nil || created.Status != StatusSuccess {
		t.Fatalf("N-CREATE: %v, %+v", err, created)
	}
	claimed, err := scu.NAction(ctx, UnifiedProcedureStepPushUID, step, UPSActionChangeState, withRetired(
		dataelem.NewDataElement(state, dataelem.CS, []byte("IN PROGRESS ")),
		dataelem.NewDataElement(txnTag, dataelem.UI, []byte(txn))))
	if err != nil || claimed.Status != StatusSuccess {
		t.Fatalf("N-ACTION: %v, %+v", err, claimed)
	}
	set, err := scu.NSet(ctx, UnifiedProcedureStepPushUID, step, withRetired(
		dataelem.NewDataElement(txnTag, dataelem.UI, []byte(txn)),
		dataelem.NewDataElement(tagNRequest, dataelem.AE, []byte("NETWORK "))))
	if err != nil || set.Status != StatusSuccess {
		t.Fatalf("N-SET: %v, %+v", err, set)
	}

	// Once as the network decodes the N-SET, once as the handler applies it.
	if n := strings.Count(serverBuf.String(), tagNRequest.String()+" (Network ID) is retired"); n != 2 {
		t.Errorf("the server's log has %d lines for %s, want 2:\n%s", n, tagNRequest, serverBuf.String())
	}
}
