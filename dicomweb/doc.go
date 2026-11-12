// Package dicomweb is a client for DICOMweb, the HTTP services of DICOM
// PS3.18: searching a service for studies, series and instances (QIDO-RS),
// retrieving them (WADO-RS) and storing them (STOW-RS).
//
// It is what a program uses to reach an archive, a cloud imaging store or a
// viewer's back end that speaks HTTP, where the network package reaches one
// that speaks DIMSE.
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
// images and bulk data on their own, the XML encoding, worklists (UPS-RS) and
// the server side of these services are not implemented.
package dicomweb
