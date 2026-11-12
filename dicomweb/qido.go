package dicomweb

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/amrshadid/go-dicom/dataset"
)

// jsonMedia is the DICOM JSON Model media type (PS3.18 8.7.3.2). Plain JSON
// is accepted beside it, which is what static stores serve.
const jsonMedia = "application/dicom+json, application/json;q=0.9"

// Query is what a QIDO-RS search asks for (PS3.18 10.6).
type Query struct {
	// Match holds the attributes to match, each named by keyword
	// ("PatientName") or by tag as eight hex digits ("00100010"), with the
	// value to match: a single value, a wildcard ("Doe*"), a range
	// ("20240101-20241231") or a list ("CT\\MR", as the server allows).
	Match map[string]string

	// IncludeFields names attributes to return beyond the ones a server
	// always does, by keyword or tag, or "all".
	IncludeFields []string

	// Limit is the most results to return, and Offset how many to skip
	// first. Zero leaves each to the server.
	Limit, Offset int

	// FuzzyMatching asks for fuzzy matching of person names, which a server
	// may not offer.
	FuzzyMatching bool
}

func (q Query) values() url.Values {
	v := url.Values{}
	for attribute, value := range q.Match {
		v.Set(attribute, value)
	}
	for _, field := range q.IncludeFields {
		v.Add("includefield", field)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		v.Set("offset", strconv.Itoa(q.Offset))
	}
	if q.FuzzyMatching {
		v.Set("fuzzymatching", "true")
	}
	return v
}

// SearchForStudies returns the studies that match q, one data set each.
func (c *Client) SearchForStudies(ctx context.Context, q Query) ([]*dataset.Dataset, error) {
	return c.search(ctx, q, "studies")
}

// SearchForSeries returns the series that match q, within the study when
// studyUID is given and across the service when it is empty.
func (c *Client) SearchForSeries(ctx context.Context, studyUID string, q Query) ([]*dataset.Dataset, error) {
	if studyUID == "" {
		return c.search(ctx, q, "series")
	}
	return c.search(ctx, q, "studies", studyUID, "series")
}

// SearchForInstances returns the instances that match q: within a series of
// a study, within a study when seriesUID is empty, or across the service
// when both are.
func (c *Client) SearchForInstances(ctx context.Context, studyUID, seriesUID string, q Query) ([]*dataset.Dataset, error) {
	switch {
	case studyUID == "" && seriesUID != "":
		// PS3.18 has no resource for a series outside its study.
		return nil, fmt.Errorf("dicomweb: instances of series %s cannot be searched for without its study", seriesUID)
	case studyUID == "":
		return c.search(ctx, q, "instances")
	case seriesUID == "":
		return c.search(ctx, q, "studies", studyUID, "instances")
	}
	return c.search(ctx, q, "studies", studyUID, "series", seriesUID, "instances")
}

func (c *Client) search(ctx context.Context, q Query, segments ...string) ([]*dataset.Dataset, error) {
	u := c.resource(segments...)
	values := q.values()
	// Encoded in a fixed order, so the same query is always the same URL.
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	query := ""
	for _, key := range keys {
		for _, value := range values[key] {
			if query != "" {
				query += "&"
			}
			query += url.QueryEscape(key) + "=" + url.QueryEscape(value)
		}
	}
	u.RawQuery = query

	// No match is 204 with no body (PS3.18 10.6.3).
	response, err := c.do(ctx, http.MethodGet, u, jsonMedia, "", nil, http.StatusOK, http.StatusNoContent,
		http.StatusPartialContent)
	if err != nil {
		return nil, err
	}
	return c.jsonDatasets(response)
}
