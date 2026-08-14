// Command dicom is a command-line interface for reading, writing, and
// networking DICOM (Digital Imaging and Communications in Medicine) data.
//
// It is the executable front end to the go-dicom library. Every subcommand is
// a thin wrapper over the packages listed under [Library packages] below, so
// anything the CLI does can also be done programmatically.
//
// # Installation
//
//	go install github.com/amrshadid/go-dicom@latest
//
// Prebuilt binaries for Linux, macOS, and Windows are attached to each release
// at https://github.com/amrshadid/go-dicom/releases.
//
// # File commands
//
// Inspect and convert DICOM files. These accept .dcm, .ima, DICOMDIR, and raw
// DICOM streams.
//
//	dicom show patient.dcm              # display the data elements
//	dicom info patient.dcm              # display file metadata
//	dicom convert patient.dcm out.json  # convert to DICOM JSON, CSV, or NIfTI
//	dicom tag-doc 0010,0010             # look up a tag in the data dictionary
//	dicom codify patient.dcm            # emit Go source that rebuilds the file
//
// # Network commands
//
// Act as a DICOM Service Class User (client) or Service Class Provider
// (server) over the DICOM Upper Layer Protocol.
//
//	dicom echoscu pacs.hospital.com:11112              # C-ECHO verification
//	dicom echoscp -port 11112                          # verification server
//	dicom storescu -aec PACS pacs:11112 study/*.dcm    # send files
//	dicom storescp -port 11112 -output ./received/     # receive and save files
//	dicom findscu -patient-name "Smith*" pacs:11112    # query
//	dicom movescu -dest MY_SCP -study 1.2.3.4 pacs:11112
//	dicom getscu -study 1.2.3.4 pacs:11112
//	dicom qrscp -port 11112                            # combined store + Q/R server
//
// Run "dicom help <command>" for the flags a given subcommand accepts.
//
// # Library packages
//
// The library is organized into focused packages. The most common entry points
// are:
//
//   - [github.com/amrshadid/go-dicom/filereader] — read DICOM files, including
//     nested sequences and encapsulated pixel data
//   - [github.com/amrshadid/go-dicom/filewriter] — write DICOM Part 10 files
//   - [github.com/amrshadid/go-dicom/dataset] — the in-memory data set, safe for
//     concurrent use
//   - [github.com/amrshadid/go-dicom/network] — SCU and SCP, DIMSE services, PDU
//     encoding, TLS
//   - [github.com/amrshadid/go-dicom/tag] — the data dictionary, 5,000+ standard
//     and 10,500+ private tags
//   - [github.com/amrshadid/go-dicom/anonymize] — de-identification per
//     DICOM PS3.15 Annex E
//
// Supporting packages cover character sets ([github.com/amrshadid/go-dicom/charset]),
// compression ([github.com/amrshadid/go-dicom/compress]), pixel data
// ([github.com/amrshadid/go-dicom/pixels]), overlays, waveforms, structured
// reports, and the DICOM JSON Model
// ([github.com/amrshadid/go-dicom/jsonrep]).
//
// # Reading a file
//
//	file, err := os.Open("patient.dcm")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer file.Close()
//
//	dicomFile, err := filereader.ReadDICOMFile(filebase.NewFileReader(file))
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	ds := dicomFile.GetDataset()
//	if elem, ok := ds.Get(tag.New(0x0010, 0x0010)); ok {
//		fmt.Printf("Patient: %s\n", elem.GetValue())
//	}
//
// # Receiving over the network
//
//	scp := network.NewSCP(network.SCPConfig{AETitle: "MY_SCP", Port: 11112})
//	scp.SetHandler(&network.StorageHandler{
//		OnStore: func(ctx context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
//			// persist, forward, or index the received instance
//			return network.StatusSuccess
//		},
//	})
//	log.Fatal(scp.ListenAndServe(context.Background()))
//
// # Handling protected health information
//
// DICOM data routinely contains protected health information. The
// [github.com/amrshadid/go-dicom/anonymize] package implements the
// de-identification profiles in DICOM PS3.15 Annex E. Note that PHI can appear
// in places a tag-based profile will not reach, including burned-in
// annotations in pixel data, private vendor tags, and structured report
// content. See SECURITY.md for the project's guidance.
//
// # Standards
//
// go-dicom implements PS3.5 (data structures and encoding), PS3.6 (the data
// dictionary), PS3.7 (message exchange), PS3.8 (network communication),
// PS3.10 (media storage and the file format), PS3.15 (security profiles), and
// the DICOM JSON Model from PS3.18.
//
// Known gaps are listed in Section 8 of CONFORMANCE.md and the Limitations
// section of the README rather than left implicit. The two most significant:
// JPEG 2000 pixel data needs a decoder you supply, and RLE Lossless is the only
// transfer syntax this library compresses to, so a C-STORE over any other
// compressed context fails rather than sending mislabelled bytes.
package main
