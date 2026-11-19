// Package dicomweb is DICOMweb, the HTTP services of DICOM PS3.18: searching
// a service for studies, series and instances (QIDO-RS), retrieving them
// (WADO-RS) and storing them (STOW-RS).
//
// [Client] is what a program uses to reach an archive, a cloud imaging store
// or a viewer's back end that speaks HTTP, where the network package reaches
// one that speaks DIMSE. [Server] is the other side: an http.Handler that
// answers the three services over an archive.
//
// # Quick start
//
//	client, err := dicomweb.NewClient("https://pacs.example.org/dicom-web",
//		dicomweb.WithBearerToken(token))
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	studies, err := client.SearchForStudies(ctx, dicomweb.Query{
//		Match: map[string]string{"PatientName": "Doe*", "StudyDate": "20240101-20241231"},
//		Limit: 20,
//	})
//
// Each result is a data set, as a C-FIND result is.
//
// # Retrieving
//
// [Client.RetrieveStudy], [Client.RetrieveSeries] and [Client.RetrieveInstance]
// return data sets. A study held whole in memory may be large, so
// [Client.RetrieveEach] hands each instance over as it arrives, as the Part 10
// file the server sent:
//
//	err := client.RetrieveEach(ctx, study, "", "", func(p dicomweb.Part) error {
//		return os.WriteFile(name(p), p.Data, 0o644)
//	})
//
// [Client.RetrieveMetadata] returns the instances without their pixel data,
// which is how a study is examined without downloading it.
//
// # Storing
//
// [Client.Store] sends Part 10 files, and [Client.StoreDatasets] data sets.
// A server answers for each instance, and may keep some and refuse others:
// the [StoreResult] says which, and why.
//
// # Limits
//
// A response is read up to [DefaultMaxResponseBytes] unless
// [WithMaxResponseBytes] says otherwise, and one that runs past the bound is
// an error. A status the request did not want is a [*StatusError], which
// carries the start of the server's explanation.
//
// The client asks for DICOM files and DICOM JSON. Retrieving frames, rendered
// images and bulk data on their own, the XML encoding and worklists (UPS-RS)
// are not implemented.
//
// # Serving
//
// [NewServer] returns a handler for the three services over a [Backend],
// which is the archive: a search asks it what a C-FIND would ask, so a study
// that matches over DIMSE matches over HTTP, a retrieve asks it for the
// files it holds, and a store gives it instances to keep. A dcmstore.Store
// is a Backend as it is.
//
//	store, err := dcmstore.Open(dir)
//	if err != nil {
//		log.Fatal(err)
//	}
//	http.Handle("/dicom-web/", http.StripPrefix("/dicom-web", dicomweb.NewServer(store)))
//
// A search names the attributes to match by keyword or by tag, as
// /studies?PatientName=Doe*&StudyDate=20240101-20241231, with includefield,
// limit and offset; an attribute inside a sequence is Sequence.Attribute. The
// answer is DICOM JSON, or 204 when nothing matches. Each result holds the
// attributes PS3.18 10.6.3.3 lists for its level that the archive has a value
// for, and any asked for with includefield.
//
// A study, a series or an instance is retrieved as multipart/related;
// type="application/dicom". With transfer-syntax=*, or a request that
// accepts anything, each instance is the file the archive holds, byte for
// byte. A request that names no transfer syntax asks for Explicit VR Little
// Endian, and an instance held compressed is decoded for it. Any other
// syntax is served only to the instances already in it. /metadata returns
// the instances as DICOM JSON with Pixel Data and other bulk values named
// and not carried.
//
// Frames of an instance, …/instances/{instance}/frames/1,2, come one part a
// frame in the order asked. A request that says nothing gets them
// uncompressed, application/octet-stream in Explicit VR Little Endian, as
// PS3.18 makes the default, a compressed frame decoded for it. With
// application/octet-stream and transfer-syntax=*, or the frame's own type
// (image/jpeg, image/jls, image/jp2, image/dicom-rle), each frame is the bytes
// the file holds. A frame is read from the file by where it lies, not by
// loading the instance. A frame number the image does not have is 404.
// Float and Double Float Pixel Data have frames too, their floats little
// endian. A frame of a 1-bit image that starts inside a byte is sent from
// the lowest bit of its first byte, as a 1-bit image of one frame is laid
// out.
//
// Metadata gives a BulkDataURI for each value it leaves out, and
// …/instances/{instance}/bulkdata/{tag}, with /{item}/{tag} for each level
// of sequence, serves it from where it lies in the file, little endian.
// Pixel Data there is its frames, joined in one part when uncompressed.
//
// The server matches literally: a request for fuzzy matching is answered
// with a warning that says so. It does not authenticate, and it serves
// whatever the archive holds to whoever reaches it, so a deployment wraps it
// in a handler that decides who may.
//
// …/rendered and …/thumbnail, of an instance or of frames, and the
// thumbnail of a series or a study, draw a frame with Dataset.RenderFrame,
// as image/jpeg or image/png. The window, viewport and quality query
// parameters are applied; what is not done, a flipped viewport region or an
// ICC profile, is refused with 400, and a study or a series rendered whole
// with 501.
//
// Instances are stored as the application/dicom parts of a multipart
// request, to /studies or into a study. Each is kept or refused on its own,
// and the answer lists both: 200 when all were kept, 202 when some were, 409
// when none was. An instance sent into a study it is not of is refused
// (A900H). A request is read up to [DefaultMaxStoreBytes] unless
// [WithMaxStoreBytes] says otherwise.
package dicomweb
