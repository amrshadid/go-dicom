package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/network"
)

// CommitSCUCommand implements the commitscu CLI command: it asks a peer to take
// storage responsibility for instances already sent to it (PS3.4 Annex J).
type CommitSCUCommand struct {
	callingAE      string
	calledAE       string
	transactionUID string
	instances      instanceList
	wait           bool
	timeout        int
}

// instanceList collects repeated -instance flags, each "sopClassUID:sopInstanceUID".
type instanceList []network.SOPInstanceReference

func (l *instanceList) String() string { return fmt.Sprintf("%d instance(s)", len(*l)) }

func (l *instanceList) Set(value string) error {
	// Split on the last colon so a UID containing none is still an error rather
	// than being silently accepted as a SOP class with an empty instance.
	i := strings.LastIndex(value, ":")
	if i <= 0 || i == len(value)-1 {
		return fmt.Errorf("expected sopClassUID:sopInstanceUID, got %q", value)
	}
	*l = append(*l, network.SOPInstanceReference{
		SOPClassUID:    value[:i],
		SOPInstanceUID: value[i+1:],
	})
	return nil
}

func (c *CommitSCUCommand) Name() string { return "commitscu" }

func (c *CommitSCUCommand) Description() string {
	return "DICOM Storage Commitment SCU (N-ACTION)"
}

func (c *CommitSCUCommand) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.callingAE, "aet", "COMMITSCU", "Calling AE title")
	fs.StringVar(&c.calledAE, "aec", "ANY-SCP", "Called AE title")
	fs.StringVar(&c.transactionUID, "transaction", "", "Transaction UID (generated if empty)")
	fs.Var(&c.instances, "instance", "Instance to commit as sopClassUID:sopInstanceUID (repeatable)")
	fs.BoolVar(&c.wait, "wait", false, "Wait for the N-EVENT-REPORT result on this association")
	fs.IntVar(&c.timeout, "timeout", 30, "Connection timeout in seconds")
}

func (c *CommitSCUCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("commitscu", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: commitscu [options] -instance CLASS:INSTANCE host:port")
	}
	if len(c.instances) == 0 {
		return fmt.Errorf("at least one -instance is required")
	}

	transactionUID := c.transactionUID
	if transactionUID == "" {
		transactionUID = network.GenerateUID()
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
	if err := scu.Associate(ctx, network.StorageCommitmentPresentationContexts()); err != nil {
		return fmt.Errorf("association failed: %w", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	fmt.Println("Association accepted")

	resp, err := scu.RequestStorageCommitment(ctx, &network.StorageCommitmentRequest{
		TransactionUID: transactionUID,
		Instances:      c.instances,
	})
	if err != nil {
		return fmt.Errorf("storage commitment request failed: %w", err)
	}

	fmt.Printf("N-ACTION accepted (status 0x%04X), transaction %s\n", resp.Status, transactionUID)
	fmt.Printf("Requested commitment for %d instance(s)\n", len(c.instances))

	if !c.wait {
		// The commitment itself arrives later, and the standard permits it on a
		// separate association. Saying so is better than implying the request
		// alone means the instances are safe to delete.
		fmt.Println("The result arrives as an N-EVENT-REPORT; use -wait to receive it here,")
		fmt.Println("or run a server to receive it on a separate association.")
		return nil
	}

	result, err := scu.ReceiveStorageCommitmentResult(ctx)
	if err != nil {
		return fmt.Errorf("waiting for the commitment result failed: %w", err)
	}

	fmt.Printf("\nResult for transaction %s\n", result.TransactionUID)
	for _, ref := range result.Successful {
		fmt.Printf("  committed: %s\n", ref.SOPInstanceUID)
	}
	for _, f := range result.Failed {
		fmt.Printf("  FAILED:    %s (reason 0x%04X)\n", f.SOPInstanceUID, f.Reason)
	}
	if len(result.Failed) > 0 {
		return fmt.Errorf("%d instance(s) were not committed", len(result.Failed))
	}
	return nil
}
