package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/network"
)

// QRSCPCommand implements the qrscp CLI command — a combined Verification,
// Storage, and Query/Retrieve SCP. Equivalent to pynetdicom's qrscp.
type QRSCPCommand struct {
	aeTitle   string
	port      int
	outputDir string
	moveDests string
}

func (c *QRSCPCommand) Name() string        { return "qrscp" }
func (c *QRSCPCommand) Description() string { return "DICOM Query/Retrieve SCP (store + find + move)" }

func (c *QRSCPCommand) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.aeTitle, "aet", "QRSCP", "AE title")
	fs.IntVar(&c.port, "port", 11112, "Listen port")
	fs.StringVar(&c.outputDir, "output", "./received", "Storage directory for received instances")
	fs.StringVar(&c.moveDests, "move-dest", "",
		"C-MOVE destinations as AETITLE=host:port, comma separated (e.g. DEST=127.0.0.1:11113)")
}

// parseMoveDestinations turns the -move-dest flag into an AE title to address
// map. A C-MOVE names its destination only by AE title, so without this there
// is nowhere to send the instances.
func parseMoveDestinations(spec string) (map[string]string, error) {
	dests := make(map[string]string)
	if spec == "" {
		return dests, nil
	}
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		aeTitle, address, found := strings.Cut(entry, "=")
		if !found || aeTitle == "" || address == "" {
			return nil, fmt.Errorf("invalid -move-dest entry %q, want AETITLE=host:port", entry)
		}
		dests[strings.TrimSpace(aeTitle)] = strings.TrimSpace(address)
	}
	return dests, nil
}

func (c *QRSCPCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("qrscp", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// The store writes the instances, indexes them, and answers the queries.
	//
	// This used to be an in-memory map with a C-FIND that returned one empty
	// identifier per stored instance and a C-GET that returned every instance it
	// held whatever was asked for — so a request for one study retrieved the whole
	// store. The instances were never written to disk at all: -output created a
	// directory and nothing was put in it.
	store, err := dcmstore.Open(c.outputDir)
	if err != nil {
		return fmt.Errorf("opening the store at %s: %w", c.outputDir, err)
	}

	moveDests, err := parseMoveDestinations(c.moveDests)
	if err != nil {
		return err
	}

	scp := network.NewSCP(network.SCPConfig{
		AETitle:          c.aeTitle,
		Port:             c.port,
		MoveDestinations: moveDests,
	})

	handler := dcmstore.NewHandler(store)
	handler.OnStored = func(_ context.Context, inst *dcmstore.Instance) error {
		fmt.Printf("  Stored: %s -> %s\n", inst.SOPInstanceUID, inst.Path)
		return nil
	}
	scp.SetHandler(handler)

	// Verification, every storage class, and the query/retrieve models. Worklist
	// is added on top, since the store does not serve it but a caller may have
	// their own handler for it on the same port.
	abstractSyntaxes := dcmstore.SupportedSOPClasses()
	abstractSyntaxes = append(abstractSyntaxes, network.AllWorklistSOPClassUIDs()...)
	scp.SetSupportedAbstractSyntaxes(abstractSyntaxes)

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
	fmt.Printf("  Indexed:   %d instance(s) already present\n", store.Count())
	fmt.Printf("  Services:  Verification, Storage, Query/Retrieve\n")
	fmt.Printf("  SOP Classes: %d\n", len(abstractSyntaxes))
	fmt.Println("Listening for associations... (Ctrl+C to stop)")

	if err := scp.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		return err
	}

	fmt.Printf("Stored %d instance(s)\n", store.Count())
	return nil
}
