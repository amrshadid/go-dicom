package dcmstore_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/dicomweb"
	"github.com/amrshadid/go-dicom/tag"
)

// A store answers a DICOMweb search as it answers a C-FIND: the server in
// package dicomweb builds the identifier from the URL and asks the store.
// These tests put a real store behind that server and search it with the
// package's own client, so they hold the two packages to each other.

// The store is the server's archive as it is, with nothing between them.
var _ dicomweb.Backend = (*dcmstore.Store)(nil)

// webClient serves a store over HTTP, mounted under a prefix as a service
// is, and returns a client for it.
func webClient(t *testing.T, store *dcmstore.Store) *dicomweb.Client {
	t.Helper()
	service := httptest.NewServer(http.StripPrefix("/dicom-web", dicomweb.NewServer(store)))
	t.Cleanup(service.Close)
	client, err := dicomweb.NewClient(service.URL + "/dicom-web")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// webText reads a value from a result the client returned. The client holds
// a value as a file would, padded to an even length (PS3.5 6.2), so the
// padding is taken off before it is compared.
func webText(t *testing.T, ds *dataset.Dataset, at tag.Tag) string {
	t.Helper()
	return strings.TrimRight(textOf(t, ds, at), " \x00")
}

// valuesOf collects one attribute across results, sorted.
func valuesOf(t *testing.T, results []*dataset.Dataset, at tag.Tag) []string {
	t.Helper()
	var out []string
	for _, result := range results {
		out = append(out, webText(t, result, at))
	}
	sort.Strings(out)
	return out
}

// TestWebSearchAtEachLevel checks the three levels over the corpus: what is
// found, and the counts a study and a series are returned with.
func TestWebSearchAtEachLevel(t *testing.T) {
	client := webClient(t, openWithCorpus(t))
	ctx := context.Background()

	studies, err := client.SearchForStudies(ctx, dicomweb.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if got := valuesOf(t, studies, tagStudyUID); strings.Join(got, " ") != "1.2.1 1.2.2" {
		t.Errorf("studies %v", got)
	}
	for _, study := range studies {
		if webText(t, study, tagStudyUID) != "1.2.1" {
			continue
		}
		// Counted over the store, as (0020,1206) and (0020,1208) are defined.
		if series := webText(t, study, tag.New(0x0020, 0x1206)); series != "2" {
			t.Errorf("the study has %s series, want 2", series)
		}
		if instances := webText(t, study, tag.New(0x0020, 0x1208)); instances != "3" {
			t.Errorf("the study has %s instances, want 3", instances)
		}
		if modalities := webText(t, study, tag.New(0x0008, 0x0061)); modalities != "CT" {
			t.Errorf("modalities in study %q", modalities)
		}
	}

	series, err := client.SearchForSeries(ctx, "1.2.1", dicomweb.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if got := valuesOf(t, series, tag.New(0x0020, 0x000E)); strings.Join(got, " ") != "1.2.1.1 1.2.1.2" {
		t.Errorf("series of the study %v", got)
	}

	instances, err := client.SearchForInstances(ctx, "1.2.1", "1.2.1.1", dicomweb.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if got := valuesOf(t, instances, tag.New(0x0008, 0x0018)); strings.Join(got, " ") != "1.2.1.1.1 1.2.1.1.2" {
		t.Errorf("instances of the series %v", got)
	}

	all, err := client.SearchForInstances(ctx, "", "", dicomweb.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(corpus) {
		t.Errorf("%d instances across the service, want %d", len(all), len(corpus))
	}
}

// TestWebSearchMatchesAsCFindDoes checks the kinds of matching a search
// names in its URL against the store's own: a wildcard, a date range, a
// list of UIDs, and nothing found.
func TestWebSearchMatchesAsCFindDoes(t *testing.T) {
	client := webClient(t, openWithCorpus(t))
	cases := []struct {
		name  string
		match map[string]string
		want  string
	}{
		{"a wildcard", map[string]string{"PatientName": "SMI*"}, "1.2.1"},
		{"a name in another case", map[string]string{"PatientName": "smi*"}, "1.2.1"},
		{"an identifier in another case", map[string]string{"PatientID": "p1"}, ""},
		{"a date range", map[string]string{"StudyDate": "20240201-20240301"}, "1.2.2"},
		{"an open date range", map[string]string{"00080020": "-20240131"}, "1.2.1"},
		{"a list of UIDs", map[string]string{"StudyInstanceUID": "1.2.2,1.2.1"}, "1.2.1 1.2.2"},
		{"modalities in the study", map[string]string{"ModalitiesInStudy": "MR"}, "1.2.2"},
		{"two keys", map[string]string{"PatientID": "P1", "StudyDate": "20240115"}, "1.2.1"},
		{"two keys that disagree", map[string]string{"PatientID": "P1", "StudyDate": "20240220"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			studies, err := client.SearchForStudies(context.Background(), dicomweb.Query{Match: c.match})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(valuesOf(t, studies, tagStudyUID), " "); got != c.want {
				t.Errorf("found %q, want %q", got, c.want)
			}
		})
	}
}

// TestWebSearchPagesInAStableOrder checks that paging through the instances
// one at a time returns each exactly once, which needs the order to be the
// same on every request.
func TestWebSearchPagesInAStableOrder(t *testing.T) {
	client := webClient(t, openWithCorpus(t))
	seen := map[string]int{}
	for offset := 0; ; offset++ {
		page, err := client.SearchForInstances(context.Background(), "", "",
			dicomweb.Query{Limit: 1, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		seen[webText(t, page[0], tag.New(0x0008, 0x0018))]++
		if offset > len(corpus) {
			t.Fatal("paging does not end")
		}
	}
	if len(seen) != len(corpus) {
		t.Errorf("paging returned %d different instances, want %d", len(seen), len(corpus))
	}
	for uid, times := range seen {
		if times != 1 {
			t.Errorf("%s was returned %d times", uid, times)
		}
	}
}

// TestWebSearchReadsANameOutsideASCII checks a name that arrived in
// ISO_IR 100: it is found by its UTF-8 spelling in the URL, returned as
// UTF-8, and the step description inside a sequence with it.
func TestWebSearchReadsANameOutsideASCII(t *testing.T) {
	client := webClient(t, storeLatin1(t))

	studies, err := client.SearchForStudies(context.Background(),
		dicomweb.Query{Match: map[string]string{"PatientName": "Buc^Jér*"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(studies) != 1 {
		t.Fatalf("%d studies", len(studies))
	}
	if got := webText(t, studies[0], tagName); got != "Buc^Jérôme" {
		t.Errorf("the name is %q", got)
	}

	series, err := client.SearchForSeries(context.Background(), "", dicomweb.Query{
		Match: map[string]string{"RequestAttributesSequence.ScheduledProcedureStepDescription": "Thorax après*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("%d series matched inside the sequence", len(series))
	}
	seq, err := series[0].GetSequence(tagRequestAttrs)
	if err != nil || seq.Length() != 1 {
		t.Fatalf("the result's Request Attributes Sequence: %v", err)
	}
	item, _ := seq.Get(0)
	if got := webText(t, item.(*dataset.Dataset), tagStepDescriptor); got != "Thorax après injection" {
		t.Errorf("the step description is %q", got)
	}
}
