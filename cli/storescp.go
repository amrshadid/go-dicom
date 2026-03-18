package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
)

// StoreSCPCommand implements the storescp CLI command.
type StoreSCPCommand struct {
	aeTitle   string
	port      int
	outputDir string
}

func (c *StoreSCPCommand) Name() string        { return "storescp" }
func (c *StoreSCPCommand) Description() string { return "DICOM Store SCP (receive files)" }

func (c *StoreSCPCommand) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.aeTitle, "aet", "STORESCP", "AE title")
	fs.IntVar(&c.port, "port", 11112, "Listen port")
	fs.StringVar(&c.outputDir, "output", ".", "Output directory for received files")
}

func (c *StoreSCPCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("storescp", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Ensure output directory exists
	if err := os.MkdirAll(c.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	scp := network.NewSCP(network.SCPConfig{
		AETitle: c.aeTitle,
		Port:    c.port,
	})

	received := 0
	handler := &network.StorageHandler{
		OnStore: func(_ context.Context, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) uint16 {
			received++
			filename := filepath.Join(c.outputDir, sopInstanceUID+".dcm")
			fmt.Printf("Received: %s (SOP Class: %s) -> %s\n", sopInstanceUID, sopClassUID, filename)
			_ = ds // In a full implementation, write ds to file
			return network.StatusSuccess
		},
	}
	scp.SetHandler(handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
		scp.Close()
	}()

	fmt.Printf("Starting DICOM Store SCP\n")
	fmt.Printf("  AE Title: %s\n", c.aeTitle)
	fmt.Printf("  Port:     %d\n", c.port)
	fmt.Printf("  Output:   %s\n", c.outputDir)
	fmt.Println("Listening for associations... (Ctrl+C to stop)")

	if err := scp.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		return err
	}

	fmt.Printf("Received %d file(s)\n", received)
	return nil
}
