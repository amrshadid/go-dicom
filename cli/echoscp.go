package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/amrshadid/go-dicom/network"
)

// EchoSCPCommand implements the echoscp CLI command.
type EchoSCPCommand struct {
	aeTitle string
	port    int
}

func (c *EchoSCPCommand) Name() string        { return "echoscp" }
func (c *EchoSCPCommand) Description() string { return "DICOM Echo SCP (verification server)" }

func (c *EchoSCPCommand) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.aeTitle, "aet", "ECHOSCP", "AE title")
	fs.IntVar(&c.port, "port", 11112, "Listen port")
}

func (c *EchoSCPCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("echoscp", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	scp := network.NewSCP(network.SCPConfig{
		AETitle: c.aeTitle,
		Port:    c.port,
	})
	scp.SetHandler(&network.EchoHandler{})
	scp.SetSupportedAbstractSyntaxes([]string{network.VerificationSOPClassUID})

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

	fmt.Printf("Starting DICOM Echo SCP\n")
	fmt.Printf("  AE Title: %s\n", c.aeTitle)
	fmt.Printf("  Port:     %d\n", c.port)
	fmt.Println("Listening for verification requests... (Ctrl+C to stop)")

	if err := scp.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}
