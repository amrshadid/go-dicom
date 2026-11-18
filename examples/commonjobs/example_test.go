package commonjobs_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strconv"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/network"
)

// readFile reads a DICOM file into a data set. It is the first step of every
// example; pydicom's dcmread.
func readFile(path string) (*dataset.Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		return nil, err
	}
	return df.GetDataset(), nil
}

// writeFile writes a data set as a DICOM file, its meta group taken from the
// data set's own SOP Class and Instance UIDs; pydicom's save_as.
func writeFile(ds *dataset.Dataset, path string) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    ds.GetStringByKeyword("SOPClassUID"),
		MediaStorageSOPInstanceUID: ds.GetStringByKeyword("SOPInstanceUID"),
		TransferSyntaxUID:          filewriter.StorageTransferSyntax(ds),
	})
	err = w.AddDataElements(filewriter.ElementsFromDataset(ds))
	if err == nil {
		err = w.Write()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Read a file and its attributes by keyword. In pydicom:
//
//	ds = pydicom.dcmread("ct.dcm")
//	ds.PatientName, ds.Modality, ds.Rows, ds.Columns
func Example_read() {
	f, err := os.Open("testdata/ct.dcm")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = f.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		fmt.Println(err)
		return
	}
	ds := df.GetDataset()

	fmt.Println("Patient:", ds.GetStringByKeyword("PatientName"))
	fmt.Println("Modality:", ds.GetStringByKeyword("Modality"))
	fmt.Println("Transfer syntax:", df.FileMetaInfo.TransferSyntaxUID)

	// Numbers stored in binary (US, SS, FL...) are read through the type that
	// knows them; the image's own attributes are gathered here.
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Size:", info.Columns, "x", info.Rows, "at", info.BitsAllocated, "bits")

	// Output:
	// Patient: Doe^Jane
	// Modality: CT
	// Transfer syntax: 1.2.840.10008.1.2.1
	// Size: 8 x 8 at 16 bits
}

// Change attributes and save the file. In pydicom:
//
//	ds.PatientName = "Roe^Richard"
//	del ds.PatientBirthDate
//	ds.save_as("changed.dcm")
func Example_modifyAndSave() {
	ds, err := readFile("testdata/ct.dcm")
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := ds.SetStringByKeyword("PatientName", "Roe^Richard"); err != nil {
		fmt.Println(err)
		return
	}
	ds.RemoveByKeyword("PatientBirthDate")

	dir, err := os.MkdirTemp("", "commonjobs")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "changed.dcm")
	if err := writeFile(ds, path); err != nil {
		fmt.Println(err)
		return
	}

	again, err := readFile(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Patient:", again.GetStringByKeyword("PatientName"))
	fmt.Println("Birth date kept:", again.ContainsByKeyword("PatientBirthDate"))

	// Output:
	// Patient: Roe^Richard
	// Birth date kept: false
}

// Read the pixels as numbers, and turn them into Hounsfield units. In
// pydicom:
//
//	arr = ds.pixel_array                   # int16, shape (8, 8)
//	hu = apply_modality_lut(arr, ds)
func Example_pixels() {
	ds, err := readFile("testdata/ct.dcm")
	if err != nil {
		fmt.Println(err)
		return
	}
	// Indexed [frame][row][column]. Pixel Representation is applied, so a
	// signed image comes back as signed numbers.
	arr, err := ds.PixelArrayInterpreted()
	if err != nil {
		fmt.Println(err)
		return
	}
	samples := arr.([][][]int16)

	// Rescale Slope and Intercept are decimal strings (DS). The fields of
	// the same name on PixelDataInfo are not filled in (#378), so they are
	// read here.
	slope, _ := strconv.ParseFloat(ds.GetStringByKeyword("RescaleSlope"), 64)
	intercept, _ := strconv.ParseFloat(ds.GetStringByKeyword("RescaleIntercept"), 64)

	first, last := samples[0][0][0], samples[0][7][7]
	fmt.Println("Frames, rows, columns:", len(samples), len(samples[0]), len(samples[0][0]))
	fmt.Println("Stored:", first, "to", last)
	fmt.Println("Hounsfield units:", float64(first)*slope+intercept, "to", float64(last)*slope+intercept)

	// Output:
	// Frames, rows, columns: 1 8 8
	// Stored: 624 to 1424
	// Hounsfield units: -400 to 400
}

// Render a frame as a viewer shows it, and save it as a PNG. In pydicom,
// with Pillow:
//
//	arr = apply_voi_lut(apply_modality_lut(ds.pixel_array, ds), ds)
//	Image.fromarray(scaled_to_8_bits(arr)).save("ct.png")
func Example_render() {
	ds, err := readFile("testdata/ct.dcm")
	if err != nil {
		fmt.Println(err)
		return
	}
	// The file's rescale and its 40/400 window. Pass
	// &dataset.RenderOptions{Window: &dataset.Window{Center: 400, Width: 1800}}
	// for another window.
	img, err := ds.RenderFrame(0, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	gray := img.(*image.Gray)
	fmt.Printf("First row: %v\n", gray.Pix[:8])
	fmt.Printf("Last row:  %v\n", gray.Pix[56:])

	var picture bytes.Buffer
	if err := png.Encode(&picture, img); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("PNG:", img.Bounds().Dx(), "x", img.Bounds().Dy())

	// Output:
	// First row: [0 0 0 0 0 0 0 0]
	// Last row:  [255 255 255 255 255 255 255 255]
	// PNG: 8 x 8
}

// De-identify by the basic profile of PS3.15 Annex E. pydicom has no
// profile of its own; this is what deid or a hand-written list of tags does
// there.
func Example_anonymize() {
	ds, err := readFile("testdata/ct.dcm")
	if err != nil {
		fmt.Println(err)
		return
	}
	original := ds.GetStringByKeyword("SOPInstanceUID")

	if err := anonymize.NewAnonymizer(anonymize.BasicProfile).Anonymize(ds); err != nil {
		fmt.Println(err)
		return
	}
	// Type 2 attributes such as the name and birth date are emptied, not
	// removed (action Z), so the data set still says they were there.
	fmt.Printf("Patient: %q\n", ds.GetStringByKeyword("PatientName"))
	fmt.Printf("Birth date: %q\n", ds.GetStringByKeyword("PatientBirthDate"))
	fmt.Printf("Institution: %q\n", ds.GetStringByKeyword("InstitutionName"))
	fmt.Println("New instance UID:", ds.GetStringByKeyword("SOPInstanceUID") != original)
	fmt.Println("Recorded as de-identified:", ds.ContainsByKeyword("PatientIdentityRemoved"))

	// Output:
	// Patient: ""
	// Birth date: ""
	// Institution: ""
	// New instance UID: true
	// Recorded as de-identified: true
}

// Walk the items of a sequence. In pydicom:
//
//	for item in ds.ProcedureCodeSequence:
//	    print(item.CodeValue, item.CodeMeaning)
func Example_sequence() {
	ds, err := readFile("testdata/ct.dcm")
	if err != nil {
		fmt.Println(err)
		return
	}
	seq, err := ds.GetSequence(ds.KeywordToTag("ProcedureCodeSequence"))
	if err != nil {
		fmt.Println(err)
		return
	}
	for i := 0; i < seq.Length(); i++ {
		element, err := seq.Get(i)
		if err != nil {
			fmt.Println(err)
			return
		}
		item := element.(*dataset.Dataset)
		fmt.Println(item.GetStringByKeyword("CodeValue"), item.GetStringByKeyword("CodeMeaning"))
	}

	// Output:
	// CTHEAD CT of the head
}

// Compress the pixel data to JPEG 2000 Lossless and back. In pydicom:
//
//	ds.compress(JPEG2000Lossless)
//	ds.decompress()
func Example_compress() {
	ds, err := readFile("testdata/ct.dcm")
	if err != nil {
		fmt.Println(err)
		return
	}
	before, _ := ds.PixelArrayInterpreted()

	if err := ds.CompressJPEG2000LosslessInPlace(); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Compressed:", ds.TransferSyntaxUID())

	if err := ds.DecompressInPlace(); err != nil {
		fmt.Println(err)
		return
	}
	after, _ := ds.PixelArrayInterpreted()
	fmt.Println("Decompressed:", ds.TransferSyntaxUID())
	fmt.Println("Every sample the same:", reflect.DeepEqual(before, after))

	// Output:
	// Compressed: 1.2.840.10008.1.2.4.90
	// Decompressed: 1.2.840.10008.1.2.1
	// Every sample the same: true
}

// Answer C-ECHO and C-STORE, and send to it. In pynetdicom:
//
//	ae.start_server(("127.0.0.1", 11112), block=False, evt_handlers=handlers)
//	assoc = ae.associate("127.0.0.1", 11112)
//	assoc.send_c_echo(); assoc.send_c_store(ds); assoc.release()
func Example_network() {
	ctx := context.Background()

	// Port 0: the system picks a free one, and Addr says which.
	received := make(chan string, 1)
	server, err := network.StartServer(ctx,
		network.SCPConfig{AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1"},
		&network.StorageHandler{
			OnStore: func(_ context.Context, _, sopInstance string, _ *dataset.Dataset) uint16 {
				received <- sopInstance
				return network.StatusSuccess
			},
		})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer server.Stop()

	scu := network.NewSCU(network.SCUConfig{CallingAE: "MY_APP", CalledAE: "ARCHIVE", Address: server.Addr()})
	if err := scu.Associate(ctx, nil); err != nil {
		fmt.Println(err)
		return
	}
	if err := scu.Echo(ctx); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("C-ECHO: success")

	ds, err := readFile("testdata/ct.dcm")
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := scu.Store(ctx, ds); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("C-STORE: the archive received", <-received)
	if err := scu.Release(ctx); err != nil {
		fmt.Println(err)
	}

	// Output:
	// C-ECHO: success
	// C-STORE: the archive received 1.2.826.0.1.3680043.10.511.1.1.3
}
