package network_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// A Service Class User associates with a peer, issues operations, then
// releases. The association must be established before any operation.
func ExampleSCU() {
	ctx := context.Background()

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "MY_APP",
		CalledAE:  "PACS",
		Address:   "pacs.hospital.com:11112",
	})

	// Passing nil proposes a default set of presentation contexts covering
	// verification, common storage SOP classes, and query/retrieve.
	if err := scu.Associate(ctx, nil); err != nil {
		log.Fatal(err)
	}
	defer scu.Release(ctx)

	// C-ECHO confirms the peer is reachable and accepts our AE title.
	if err := scu.Echo(ctx); err != nil {
		log.Fatal(err)
	}
	fmt.Println("peer is reachable")
}

// C-STORE sends an instance. The data set must carry SOP Class UID
// (0008,0016) and SOP Instance UID (0008,0018); the presentation context is
// selected from the former.
func ExampleSCU_Store() {
	ctx := context.Background()

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "MY_APP",
		CalledAE:  "PACS",
		Address:   "pacs.hospital.com:11112",
	})
	if err := scu.Associate(ctx, nil); err != nil {
		log.Fatal(err)
	}
	defer scu.Release(ctx)

	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016),
		dataelem.UI, []byte(network.CTImageStorageUID)))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018),
		dataelem.UI, []byte("1.2.3.4.5.6.7.8.9")))

	if err := scu.Store(ctx, ds); err != nil {
		log.Fatal(err)
	}
}

// C-FIND results stream on a channel, because a query can match arbitrarily
// many instances and the peer sends them one at a time. The channel closes
// when the peer sends its final status; always drain it or cancel the context.
func ExampleSCU_Find() {
	ctx := context.Background()

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "MY_APP",
		CalledAE:  "PACS",
		Address:   "pacs.hospital.com:11112",
	})
	if err := scu.Associate(ctx, nil); err != nil {
		log.Fatal(err)
	}
	defer scu.Release(ctx)

	// A query is a data set whose empty attributes are the ones to return.
	query := dataset.NewDataset()
	_ = query.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052),
		dataelem.CS, []byte("STUDY ")))
	_ = query.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010),
		dataelem.PN, []byte("Smith*")))
	_ = query.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000D),
		dataelem.UI, nil)) // requested, returned by the peer

	results, err := scu.Find(ctx, query)
	if err != nil {
		log.Fatal(err)
	}

	for r := range results {
		if r.Err != nil {
			log.Fatal(r.Err)
		}
		if elem, ok := r.DataSet.Get(tag.New(0x0020, 0x000D)); ok {
			fmt.Printf("study: %s\n", elem.GetValue())
		}
	}
}

// A Service Class Provider listens for associations and dispatches each
// request to a Handler. ListenAndServe blocks, handling each association in
// its own goroutine.
func ExampleSCP() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scp := network.NewSCP(network.SCPConfig{
		AETitle: "MY_SCP",
		Port:    11112,
		// Bound concurrency on an exposed server. Connections beyond the
		// limit are rejected with an A-ASSOCIATE-RJ.
		MaxAssociations: 32,
	})

	scp.SetHandler(&network.StorageHandler{
		OnStore: func(ctx context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
			// The handler runs on the association's goroutine, so hand long
			// work to a queue rather than blocking here.
			fmt.Printf("received %s\n", sopInstance)
			return network.StatusSuccess
		},
	})

	if err := scp.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

// A handler can read the association's details from the request context
// without changing the Handler interface.
func ExampleAssociationInfoFromContext() {
	scp := network.NewSCP(network.SCPConfig{AETitle: "MY_SCP", Port: 11112})

	scp.SetHandler(&network.StorageHandler{
		OnStore: func(ctx context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
			// Returns nil when called outside an association, so check it.
			if info := network.AssociationInfoFromContext(ctx); info != nil {
				fmt.Printf("from %s at %s (%s)\n",
					info.CallingAE, info.RemoteAddr, info.PeerImplementationVersion)
			}
			return network.StatusSuccess
		},
	})
}

// Extended negotiation items are proposed alongside the presentation contexts.
// Role selection is what permits a peer to send C-STORE back over an
// association this AE initiated, which C-GET requires.
func ExampleExtendedNegotiation() {
	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "MY_APP",
		CalledAE:  "PACS",
		Address:   "pacs.hospital.com:11112",
		Network: network.NetworkConfig{
			NetworkTimeout: 30 * time.Second,
		},
		ExtendedNegotiation: &network.ExtendedNegotiation{
			RoleSelections: []network.SCPSCURoleSelection{
				{SOPClassUID: network.CTImageStorageUID, SCURole: true, SCPRole: true},
			},
			UserIdentity: &network.UserIdentityNegotiation{
				Type:           network.UserIdentityUsernamePassword,
				PrimaryField:   []byte("operator"),
				SecondaryField: []byte("password"),
			},
		},
	})

	if err := scu.Associate(context.Background(), nil); err != nil {
		log.Fatal(err)
	}
	defer scu.Release(context.Background())

	// Inspect what the peer actually agreed to.
	if role, ok := scu.Association().RoleSelectionFor(network.CTImageStorageUID); ok {
		fmt.Println("peer accepted SCP role:", role.SCPRole)
	}
}

// Data sets are encoded with the transfer syntax negotiated for the
// presentation context they travel on, not a fixed one. Encoding with a
// syntax the peer did not agree to produces a data set it cannot parse.
func ExampleEncodeDataset() {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010),
		dataelem.PN, []byte("Doe^John")))

	for _, ts := range []string{
		network.ImplicitVRLittleEndianUID,
		network.ExplicitVRLittleEndianUID,
		network.ExplicitVRBigEndianUID,
	} {
		encoded, err := network.EncodeDataset(ds, ts)
		if err != nil {
			log.Fatal(err)
		}

		// Decoding with the same syntax recovers the value.
		decoded, err := network.DecodeDataset(encoded, ts)
		if err != nil {
			log.Fatal(err)
		}
		elem, _ := decoded.Get(tag.New(0x0010, 0x0010))
		fmt.Printf("%-22s %2d bytes -> %s\n", ts, len(encoded), elem.GetValue())
	}

	// Output:
	// 1.2.840.10008.1.2      16 bytes -> Doe^John
	// 1.2.840.10008.1.2.1    16 bytes -> Doe^John
	// 1.2.840.10008.1.2.2    16 bytes -> Doe^John
}
