# Coming from pydicom

The calls you know from pydicom and pynetdicom, and what does the same job
here. Each row links to a runnable example in
[`examples/commonjobs`](../examples/commonjobs/example_test.go) where there is
one; [Examples](examples.md) shows the same jobs in full, with the command
line beside them.

Three differences shape everything else:

- **Values are not Python objects.** A data set holds elements keyed by tag;
  a value is the bytes or string read from the file. Text attributes read
  with `GetStringByKeyword`; numbers stored in binary (US, SS, UL, FL, FD) read
  through the element (`elem.GetValue()`) or through a type that knows them,
  such as `GetPixelDataInfo` for the image attributes.
- **Errors are returned, not raised.** Every call that can fail says so.
- **One static binary.** The command line (`go-dicom`) does what pydicom's
  `pydicom show`, pynetdicom's applications and a fair part of dcmtk do, with
  nothing to install beside it.

## Files and attributes

| pydicom | go-dicom | Example |
|---|---|---|
| `ds = pydicom.dcmread("ct.dcm")` | `df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))`, then `ds := df.GetDataset()` | `Example_read` |
| `ds.file_meta.TransferSyntaxUID` | `df.FileMetaInfo.TransferSyntaxUID` | `Example_read` |
| `ds.PatientName` | `ds.GetStringByKeyword("PatientName")` | `Example_read` |
| `ds[0x0010, 0x0010]` | `ds.Get(tag.New(0x0010, 0x0010))` | |
| `"PatientName" in ds` | `ds.ContainsByKeyword("PatientName")` | `Example_modifyAndSave` |
| `ds.PatientName = "Roe^Richard"` | `ds.SetStringByKeyword("PatientName", "Roe^Richard")` | `Example_modifyAndSave` |
| `del ds.PatientBirthDate` | `ds.RemoveByKeyword("PatientBirthDate")` | `Example_modifyAndSave` |
| `ds.Rows`, `ds.Columns`, `ds.BitsAllocated` | `info, err := ds.GetPixelDataInfo()`; `info.Rows`, `info.Columns`, `info.BitsAllocated` | `Example_read` |
| `ds.save_as("out.dcm")` | `filewriter.NewDICOMFileWriter`, `SetFileMetaInfo`, `AddDataElements(filewriter.ElementsFromDataset(ds))`, `Write` | `Example_modifyAndSave` |
| `Dataset()` and attribute assignment | `dataset.NewDataset()`, `ds.Add(dataelem.NewDataElement(tag, vr, value))` | |
| `for item in ds.ProcedureCodeSequence` | `seq, err := ds.GetSequence(ds.KeywordToTag("ProcedureCodeSequence"))`, `seq.Get(i)` | `Example_sequence` |
| `ds.walk(callback)` | `ds.ForEach(func(elem *dataelem.DataElement) error { ... })`, which does not descend into sequences | |
| `ds.to_json()` | `ds.ToDICOMJSONString()` | |
| `pydicom.datadict.keyword_for_tag(...)` | `ds.TagToKeyword(t)`, `tag.GlobalDictionary()` | |
| `dcmread(..., defer_size=...)` | `filereader.ReadDICOMFileWithOptions(r, filereader.ReadOptions{SkipBulkData: true})`: bulk values stay in the file | |

## Pixels

| pydicom | go-dicom | Example |
|---|---|---|
| `ds.pixel_array` | `ds.PixelArrayInterpreted()`: `[frame][row][column]`, signed when Pixel Representation says so | `Example_pixels` |
| `ds.pixel_array` for color, last axis the sample | `ds.PixelArrayBySample()` | |
| `apply_modality_lut(arr, ds)` | slope and intercept applied by hand, or `RenderFrame` for a picture | `Example_pixels` |
| `apply_voi_lut(...)`, then Pillow's `Image.fromarray(...).save("x.png")` | `img, err := ds.RenderFrame(0, nil)`; `png.Encode(w, img)` | `Example_render` |
| `ds.decompress()` | `ds.DecompressInPlace()` | `Example_compress` |
| `ds.compress(JPEG2000Lossless)` | `ds.CompressJPEG2000LosslessInPlace()` | `Example_compress` |
| `ds.compress(JPEGBaseline8Bit)` | `ds.CompressJPEGBaselineInPlace(quality)` | |

`RenderFrame` applies the sign of the samples, the rescale and the window (the
file's own, or the one given), MONOCHROME1, YBR and palette color, at 8 bits.
CONFORMANCE.md §8.1 says what it does not apply.

## De-identification

pydicom has no de-identification profile of its own; `deid` or a hand-made
list of tags is the usual way. Here it is built in:

| Job | go-dicom | Example |
|---|---|---|
| The basic profile of PS3.15 Annex E | `anonymize.NewAnonymizer(anonymize.BasicProfile).Anonymize(ds)` | `Example_anonymize` |
| On the command line | `go-dicom anonymize in.dcm out.dcm`, `-profile retain-dates` and the other options | |
| Text burned into the pixels | `go-dicom anonymize -detect`, `share -preview`, `-redact x,y,w,h` or `-redact auto` | |

## Networking (pynetdicom)

| pynetdicom | go-dicom | Example |
|---|---|---|
| `ae = AE(); assoc = ae.associate(host, port)` | `scu := network.NewSCU(network.SCUConfig{...})`; `scu.Associate(ctx, nil)` | `Example_network` |
| `assoc.send_c_echo()` | `scu.Echo(ctx)` | `Example_network` |
| `assoc.send_c_store(ds)` | `scu.Store(ctx, ds)` | `Example_network` |
| `assoc.send_c_find(ds, model)` | `results, err := scu.Find(ctx, query)`: the matches arrive on a channel | |
| `assoc.send_c_move(ds, dest, model)` | `scu.Move(ctx, query, dest)` | |
| `assoc.send_c_get(ds, model)` | `scu.Get(ctx, query)`, with `SCUConfig.OnCStore` for what arrives | |
| `assoc.release()` | `scu.Release(ctx)` | `Example_network` |
| `ae.start_server(addr, evt_handlers=...)` | `network.StartServer(ctx, network.SCPConfig{...}, handler)`, or `NewSCP(...).ListenAndServe(ctx)` to block | `Example_network` |
| `evt.EVT_C_STORE` handler | `&network.StorageHandler{OnStore: ...}`, or any type with `HandleCStore` | `Example_network` |
| `python -m pynetdicom echoscu host port` | `go-dicom echoscu host:port` | |
| `python -m pynetdicom storescu`, `storescp`, `findscu`, `movescu`, `getscu`, `qrscp` | `go-dicom storescu`, `storescp`, `findscu`, `movescu`, `getscu`, `qrscp` | |

The command lines take `host:port` as one argument, and `-aec` for the called
AE title. [Examples](examples.md#send-receive-query-and-retrieve) has each one
as it was run.
