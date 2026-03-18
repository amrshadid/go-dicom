package cli

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/amrshadid/go-dicom/network"
)

// EchoSCUCommand implements the echoscu CLI command.
type EchoSCUCommand struct {
	callingAE string
	calledAE  string
	timeout   int
}

func (c *EchoSCUCommand) Name() string        { return "echoscu" }
func (c *EchoSCUCommand) Description() string { return "DICOM Echo SCU (verification)" }

func (c *EchoSCUCommand) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.callingAE, "aet", "ECHOSCU", "Calling AE title")
	fs.StringVar(&c.calledAE, "aec", "ANY-SCP", "Called AE title")
	fs.IntVar(&c.timeout, "timeout", 30, "Connection timeout in seconds")
}

func (c *EchoSCUCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("echoscu", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: echoscu [options] host:port")
	}

	address := fs.Arg(0)

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: c.callingAE,
		CalledAE:  c.calledAE,
		Address:   address,
		Network: network.NetworkConfig{
			NetworkTimeout: time.Duration(c.timeout) * time.Second,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.timeout)*time.Second)
	defer cancel()

	fmt.Printf("Requesting Association with %s (AE: %s)\n", address, c.calledAE)

	if err := scu.Associate(ctx, network.DefaultVerificationContexts()); err != nil {
		return fmt.Errorf("association failed: %w", err)
	}
	defer scu.Release(ctx)

	fmt.Println("Association accepted")
	fmt.Println("Sending C-ECHO...")

	if err := scu.Echo(ctx); err != nil {
		return fmt.Errorf("C-ECHO failed: %w", err)
	}

	fmt.Println("C-ECHO response: Success (0x0000)")
	fmt.Println("Releasing Association")

	return nil
}
