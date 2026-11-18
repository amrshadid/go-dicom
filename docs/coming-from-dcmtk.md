# Coming from dcmtk

The dcmtk commands you know, and the `go-dicom` command that does the same
job, with what differs. Where there is no command, the library call is named;
[Examples](examples.md) shows each job in full, and every command line there
was run on a file from pydicom's test corpus.

The network commands keep dcmtk's names. Two habits change:

- **The peer is one argument, `host:port`**, not two.
- **The called AE title is `-aec`, the calling one `-aet`**, as in dcmtk, but
  a server here answers only to its own title (each server command's name,
  `STORESCP`, `QRSCP`..., unless `-aet` sets another), so `-aec` has to match
  it.

## Files

| dcmtk | go-dicom | Notes |
|---|---|---|
| `dcmdump file.dcm` | `go-dicom show file.dcm` | `--top` for the top level, `--exclude-private` |
| `dcmdump +L file.dcm` | `go-dicom show file.dcm --full` | Whole values, bulk data still a count of bytes |
| `dcmdump +P 0010,0010 file.dcm` | `go-dicom show file.dcm::0010,0010` | The tag; a keyword is not taken yet ([#384](https://github.com/amrshadid/go-dicom/issues/384)) |
| (dcmdump prints no positions) | `go-dicom show file.dcm --json` | Every element with the offsets of its tag, VR, length and value in the file |
| `dcm2json file.dcm` | `go-dicom convert file.dcm out.json` | |
| `dcm2xml` | none | |
| `dcm2pnm`, `dcmj2pnm`, `dcm2img` with `+on` / `+oj` | `go-dicom convert file.dcm out.png --format png` (or `--format jpeg --quality 90`) | The file's window is applied by default, where dcm2pnm applies none unless `+Wi 1` |
| `dcm2pnm +Ww 400 1800` | `go-dicom convert file.dcm out.png --format png --window 400,1800` | |
| `dcm2pnm +Fa` (all frames) | `go-dicom convert file.dcm out.png --format png --frame all` | `out-0001.png`, `out-0002.png`... |
| `dcmodify -m "(0010,0010)=Roe^Richard"` | none; in Go `ds.SetStringByKeyword` and a file writer | `Example_modifyAndSave` |
| `dcmodify -ea`, or a script of `dcmodify` calls to de-identify | `go-dicom anonymize in.dcm out.dcm` | The PS3.15 Annex E basic profile and its options (`-profile`); directories too |
| (no dcmtk tool) | `go-dicom anonymize -redact x,y,w,h`, `-redact auto`, `-detect`; `go-dicom share` | Text burned into the pixels |
| `dcmcjpeg`, `dcmcrle`, `dcmcjpls` | none; in Go `ds.CompressJPEGBaselineInPlace(q)`, `ds.CompressJPEG2000LosslessInPlace()` | `Example_compress`. JPEG-LS and RLE are decoded and not written by a command |
| `dcmdjpeg`, `dcmdrle`, `dcmdjpls`, `dcmconv +te` | none; in Go `ds.DecompressInPlace()` | `Example_compress` |
| `dcmmkdir` | none; in Go package `fileset`, `FileSet.WriteDICOMDIR` | |
| dcmtk's data dictionary (`dicom.dic`) | `go-dicom tag-doc 0010,0010` | Name, keyword, VR, VM and status of a tag |
| (no dcmtk tool) | `go-dicom codify file.dcm` | Go source that recreates the file |

## Network

| dcmtk | go-dicom |
|---|---|
| `echoscu host port` | `go-dicom echoscu -aec ECHOSCP host:port` |
| `storescp -od received port` | `go-dicom storescp -port 11113 -output received` |
| `storescu -aec STORESCP host port files...` | `go-dicom storescu -aec STORESCP host:port files...` |
| `findscu -S -k QueryRetrieveLevel=STUDY -k PatientName=Smith* host port` | `go-dicom findscu -aec QRSCP -level STUDY -patient-name "Smith*" host:port` |
| `findscu -k 0020,000D=1.2.3 -k 0008,0060` | `go-dicom findscu -level SERIES -key 0020,000D=1.2.3 -key 0008,0060 host:port` |
| `movescu -S -aem DEST -k StudyInstanceUID=1.2.3 host port` | `go-dicom movescu -aec QRSCP -dest DEST -study 1.2.3 host:port` |
| `getscu -S -k StudyInstanceUID=1.2.3 host port` | `go-dicom getscu -aec QRSCP -study 1.2.3 -output retrieved host:port` |
| `dcmqrscp` with `dcmqrscp.cfg` | `go-dicom qrscp -port 11114 -output archive -move-dest DEST=host:port` | 
| `dcmsend host port files...` | `go-dicom storescu -aec STORESCP host:port study/*.dcm` |
| `+tls key cert` on any network tool | `-tls -tls-cert node.crt -tls-key node.key -tls-ca ca.crt` |
| `-d` or `-ll trace` (a log of the association) | `-events trace.jsonl`: every PDU with its bytes, the contexts, every DIMSE message, as JSON lines |

`qrscp` needs no configuration file: it stores what it is sent, indexes it,
and answers C-FIND, C-MOVE and C-GET against what it holds. `-move-dest`
names the C-MOVE destinations, and `-web 8080` serves the same archive over
DICOMweb, which `go-dicom qido`, `wado` and `stow` reach (dcmtk has no
DICOMweb tools).

## As a library

dcmtk's C++ classes and the Go calls that do the same job:

| dcmtk | go-dicom |
|---|---|
| `DcmFileFormat::loadFile` | `filereader.ReadDICOMFile(filebase.NewFileReader(f))` |
| `DcmDataset::findAndGetOFString(DCM_PatientName, s)` | `ds.GetStringByKeyword("PatientName")` |
| `DcmDataset::putAndInsertString` | `ds.SetStringByKeyword` |
| `DcmFileFormat::saveFile` | `filewriter.NewDICOMFileWriter(...)`, `AddDataElements(filewriter.ElementsFromDataset(ds))`, `Write()` |
| `DicomImage` and `writeBMP` / `writePPM` | `ds.RenderFrame(frame, opts)`, an `image.Image` for `image/png` or `image/jpeg` |
| `DcmSCU` | `network.NewSCU` |
| `DcmSCP`, `DcmStorageSCP` | `network.StartServer` with a handler, such as `network.StorageHandler` |

Each is shown, running, in [`examples/commonjobs`](../examples/commonjobs/example_test.go).
