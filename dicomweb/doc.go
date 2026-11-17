// Package dicomweb is DICOMweb, the HTTP services of DICOM PS3.18: searching
// a service for studies, series and instances (QIDO-RS), retrieving them
// (WADO-RS) and storing them (STOW-RS).
//
// [Client] is what a program uses to reach an archive, a cloud imaging store
// or a viewer's back end that speaks HTTP, where the network package reaches
// one that speaks DIMSE. [Server] is the other side: an http.Handler that
// answers the search service over an archive.
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
// [NewServer] returns a handler for the search service over a [Backend],
// which is the archive: it is asked what a C-FIND would ask, so a study that
// matches over DIMSE matches over HTTP. A dcmstore.Store is a Backend as it
// is.
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
// The server matches literally: a request for fuzzy matching is answered
// with a warning that says so. It does not authenticate, and it serves
// whatever the archive holds to whoever reaches it, so a deployment wraps it
// in a handler that decides who may. Retrieving and storing are not served
// yet.
package dicomweb
