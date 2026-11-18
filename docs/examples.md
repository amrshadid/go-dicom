# Examples: the common jobs

Each job is shown twice: as a command line, and as Go. Every Go sample here is
taken from an `Example` function in
[`examples/commonjobs`](../examples/commonjobs/example_test.go), named beside
it, with the error handling shortened; `go test ./examples/commonjobs` runs
each function and checks what it prints. Every command line was run on a
file from pydicom's test corpus (`CT_small.dcm` as `ct.dcm` and
`study/ct.dcm`, `MR_small.dcm` as `study/mr.dcm`, `examples_rgb_color.dcm`,
`SC_rgb_rle_2frame.dcm`), and the output shown is what it printed.

Build the command line once:

```bash
go build -o go-dicom .
```

Readers who know pydicom or dcmtk will find the same jobs indexed by the
calls and commands they know in [Coming from pydicom](coming-from-pydicom.md)
and [Coming from dcmtk](coming-from-dcmtk.md).

## Read a file

```console
$ go-dicom info ct.dcm
=== DICOM File Information ===
File:         ct.dcm
Size:         39206 bytes
...
Patient Name:    CompressedSamples^CT1
Patient ID:      1234ABCD
Study Date:      20040119
Modality:        CT
$ go-dicom show ct.dcm              # every element, as stored
$ go-dicom show ct.dcm::0010,0010   # one element, whole
0010,0010    PN  22      CompressedSamples^CT1
$ go-dicom show ct.dcm --json       # every element, with where it sits in the file
```

`show file::Element` takes the tag. A keyword such as `PatientName` is not
taken yet ([#384](https://github.com/amrshadid/go-dicom/issues/384)).

In Go (`Example_read`):

```go
f, err := os.Open("testdata/ct.dcm")
if err != nil {
	return err
}
defer f.Close()

df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
if err != nil {
	return err
}
ds := df.GetDataset()

fmt.Println("Patient:", ds.GetStringByKeyword("PatientName"))
fmt.Println("Transfer syntax:", df.FileMetaInfo.TransferSyntaxUID)
info, err := ds.GetPixelDataInfo() // Rows, Columns, Bits Allocated and the rest
```

## Change attributes and save the file

There is no command for this: `codify` writes the Go program that recreates a
file, which is a place to start. In Go (`Example_modifyAndSave`):

```go
if err := ds.SetStringByKeyword("PatientName", "Roe^Richard"); err != nil {
	return err
}
ds.RemoveByKeyword("PatientBirthDate")

out, err := os.Create("changed.dcm")
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
```

`ElementsFromDataset` carries nested sequence items with it.

## Read the pixels as numbers

In Go (`Example_pixels`):

```go
arr, err := ds.PixelArrayInterpreted() // [frame][row][column], signed when the image is
samples := arr.([][][]int16)

slope, _ := strconv.ParseFloat(ds.GetStringByKeyword("RescaleSlope"), 64)
intercept, _ := strconv.ParseFloat(ds.GetStringByKeyword("RescaleIntercept"), 64)
hu := float64(samples[0][0][0])*slope + intercept
```

`PixelArrayInterpreted` decodes compressed pixel data first. `PixelArrayBySample`
keeps color samples apart in the last dimension.
`PixelDataInfo.RescaleSlope` and `RescaleIntercept` are not filled in
([#378](https://github.com/amrshadid/go-dicom/issues/378)), which is why the
sample reads the attributes.

## Make a picture

```console
$ go-dicom convert ct.dcm ct.png --format png
Wrote ct.png
$ go-dicom convert ct.dcm ct-bone.png --format png --window 400,1800
Wrote ct-bone.png
$ go-dicom convert ct.dcm ct.jpg --format jpeg --quality 90
Wrote ct.jpg
$ go-dicom convert SC_rgb_rle_2frame.dcm frames.png --format png --frame all
Wrote frames-0001.png
Wrote frames-0002.png
```

The picture is what a viewer shows: rescaled, windowed (the file's own window
unless `--window` gives one), MONOCHROME1 inverted, color converted to RGB, at
8 bits. In Go (`Example_render`):

```go
img, err := ds.RenderFrame(0, nil) // or &dataset.RenderOptions{Window: &dataset.Window{Center: 400, Width: 1800}}
if err != nil {
	return err
}
err = png.Encode(out, img)
```

## De-identify

```console
$ go-dicom anonymize ct.dcm ct-anon.dcm
De-identified 1 file(s)
$ go-dicom show ct-anon.dcm::0010,0010
0010,0010    PN  0
$ go-dicom anonymize -profile retain-dates study/ study-anon/
De-identified 2 file(s)
```

The basic profile of PS3.15 Annex E, with any of its options on top. In Go
(`Example_anonymize`):

```go
err := anonymize.NewAnonymizer(anonymize.BasicProfile).Anonymize(ds)
```

Text burned into the pixels is not an attribute. A detector can find it; it
can miss, so look at what it found before anything leaves you:

```console
$ go-dicom anonymize -detect examples_rgb_color.dcm
248,10,63,43
8,11,78,42
123,179,28,10
158,179,23,10
198,227,60,10
241,227,70,10
$ go-dicom share -preview examples_rgb_color.dcm check.png
Wrote check.png
...
$ go-dicom anonymize -redact auto examples_rgb_color.dcm us-anon.dcm
  examples_rgb_color.dcm: 6 region(s) detected and filled
De-identified 1 file(s)
$ go-dicom share -redact 0,0,320,56 examples_rgb_color.dcm us.png
Wrote us.png
```

## Walk a sequence

In Go (`Example_sequence`):

```go
seq, err := ds.GetSequence(ds.KeywordToTag("ProcedureCodeSequence"))
if err != nil {
	return err
}
for i := 0; i < seq.Length(); i++ {
	element, _ := seq.Get(i)
	item := element.(*dataset.Dataset)
	fmt.Println(item.GetStringByKeyword("CodeValue"), item.GetStringByKeyword("CodeMeaning"))
}
```

## Compress and decompress

There is no command that rewrites a file in another transfer syntax.
`storescu` compresses on the way when the peer asks for it. In Go
(`Example_compress`):

```go
err := ds.CompressJPEG2000LosslessInPlace() // or CompressJPEGBaselineInPlace(90), lossy, 8-bit only
...
err = ds.DecompressInPlace()
```

## Send, receive, query and retrieve

```console
$ go-dicom echoscp -port 11113 &
$ go-dicom echoscu -aec ECHOSCP 127.0.0.1:11113
Requesting Association with 127.0.0.1:11113 (AE: ECHOSCP)
Association accepted
Sending C-ECHO...
C-ECHO response: Success (0x0000)
Releasing Association

$ go-dicom storescp -port 11114 -output received &
$ go-dicom storescu -aec STORESCP 127.0.0.1:11114 study/ct.dcm study/mr.dcm
...
Results: 2 sent, 0 failed

$ go-dicom qrscp -port 11115 -output archive -move-dest STORESCP=127.0.0.1:11114 -web 8080 &
$ go-dicom storescu -aec QRSCP 127.0.0.1:11115 study/ct.dcm study/mr.dcm
$ go-dicom findscu -aec QRSCP -level STUDY -patient-name "CompressedSamples*" 127.0.0.1:11115
--- Result 1 ---
  Study Date                               20040119
  Query/Retrieve Level                     STUDY
  Patient's Name                           CompressedSamples^CT1
  ...
$ go-dicom findscu -aec QRSCP -level SERIES -key 0020,000D=1.3.6.1.4.1.5962.1.2.1.20040119072730.12322 -key 0008,0060 127.0.0.1:11115
$ go-dicom movescu -aec QRSCP -dest STORESCP -study 1.3.6.1.4.1.5962.1.2.1.20040119072730.12322 127.0.0.1:11115
Moving from 127.0.0.1:11115 to STORESCP (level: STUDY)
C-MOVE completed successfully
$ go-dicom getscu -aec QRSCP -study 1.3.6.1.4.1.5962.1.2.1.20040119072730.12322 -output retrieved 127.0.0.1:11115
C-GET completed: 1 instance(s) retrieved to retrieved
```

The called AE title (`-aec`) has to be the one the server answers to: each
server command's own name unless `-aet` sets another.

The same archive over DICOMweb, because `qrscp` was given `-web 8080`:

```console
$ go-dicom qido -match "PatientName=CompressedSamples*" http://127.0.0.1:8080/dicom-web
$ go-dicom wado -study 1.3.6.1.4.1.5962.1.2.1.20040119072730.12322 http://127.0.0.1:8080/dicom-web wado-out
Retrieved 1 instance(s) to wado-out
$ go-dicom stow http://127.0.0.1:8080/dicom-web study/
Stored 2 instance(s)
```

In Go, a server and a client in one program (`Example_network`):

```go
server, err := network.StartServer(ctx,
	network.SCPConfig{AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1"}, // port 0: the system picks
	&network.StorageHandler{
		OnStore: func(_ context.Context, _, sopInstance string, _ *dataset.Dataset) uint16 {
			return network.StatusSuccess
		},
	})
if err != nil {
	return err
}
defer server.Stop()

scu := network.NewSCU(network.SCUConfig{CallingAE: "MY_APP", CalledAE: "ARCHIVE", Address: server.Addr()})
if err := scu.Associate(ctx, nil); err != nil {
	return err
}
defer scu.Release(ctx)
err = scu.Echo(ctx)
...
err = scu.Store(ctx, ds)
```
