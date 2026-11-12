package dicomweb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// Part is one instance of a WADO-RS response: a DICOM Part 10 file.
type Part struct {
	// Data is the file, as the server sent it.
	Data []byte
	// ContentType is the part's own media type, which may name its
	// transfer syntax.
	ContentType string
}

// File parses the part.
func (p Part) File() (*filereader.DICOMFile, error) {
	file, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(p.Data)))
	if err != nil {
		return nil, fmt.Errorf("dicomweb: a retrieved part is not a DICOM file: %w", err)
	}
	return file, nil
}

// Dataset parses the part and returns its data set, which knows the
// transfer syntax it was sent in.
func (p Part) Dataset() (*dataset.Dataset, error) {
	file, err := p.File()
	if err != nil {
		return nil, err
	}
	return file.GetDataset(), nil
}

// RetrieveEach retrieves instances and hands each to fn as it arrives, so a
// study need not be held whole: the instances of a study, of one of its
// series when seriesUID is given, or one instance when instanceUID is too.
// An error from fn stops the retrieval and is returned.
func (c *Client) RetrieveEach(ctx context.Context, studyUID, seriesUID, instanceUID string, fn func(Part) error) error {
	segments, err := instancePath(studyUID, seriesUID, instanceUID)
	if err != nil {
		return err
	}
	// The instances as Part 10 files, each a part of a multipart response
	// (PS3.18 10.4). transfer-syntax=* asks for each as it is stored.
	accept := `multipart/related; type="application/dicom"; transfer-syntax=` + c.transfer
	response, err := c.do(ctx, http.MethodGet, c.resource(segments...), accept, "", nil,
		http.StatusOK, http.StatusPartialContent)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	contentType := response.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("dicomweb: the response's Content-Type %q: %w", contentType, err)
	}
	body := c.limited(response.Body)

	// A server answering for one instance may send the file itself.
	if mediaType == "application/dicom" {
		data, err := io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("dicomweb: reading the response: %w", err)
		}
		return fn(Part{Data: data, ContentType: contentType})
	}
	if mediaType != "multipart/related" || params["boundary"] == "" {
		return fmt.Errorf("dicomweb: the response is %q, not multipart/related with a boundary", contentType)
	}

	reader := multipart.NewReader(body, params["boundary"])
	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("dicomweb: reading the response: %w", err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			return fmt.Errorf("dicomweb: reading the response: %w", err)
		}
		if err := fn(Part{Data: data, ContentType: part.Header.Get("Content-Type")}); err != nil {
			return err
		}
	}
}

// RetrieveStudy retrieves every instance of a study.
func (c *Client) RetrieveStudy(ctx context.Context, studyUID string) ([]*dataset.Dataset, error) {
	return c.retrieve(ctx, studyUID, "", "")
}

// RetrieveSeries retrieves every instance of a series.
func (c *Client) RetrieveSeries(ctx context.Context, studyUID, seriesUID string) ([]*dataset.Dataset, error) {
	if seriesUID == "" {
		return nil, fmt.Errorf("dicomweb: no Series Instance UID given")
	}
	return c.retrieve(ctx, studyUID, seriesUID, "")
}

// RetrieveInstance retrieves one instance.
func (c *Client) RetrieveInstance(ctx context.Context, studyUID, seriesUID, instanceUID string) (*dataset.Dataset, error) {
	if seriesUID == "" || instanceUID == "" {
		return nil, fmt.Errorf("dicomweb: an instance is named by its study, series and instance UIDs")
	}
	instances, err := c.retrieve(ctx, studyUID, seriesUID, instanceUID)
	if err != nil {
		return nil, err
	}
	if len(instances) != 1 {
		return nil, fmt.Errorf("dicomweb: the server sent %d instances for one", len(instances))
	}
	return instances[0], nil
}

func (c *Client) retrieve(ctx context.Context, studyUID, seriesUID, instanceUID string) ([]*dataset.Dataset, error) {
	var out []*dataset.Dataset
	err := c.RetrieveEach(ctx, studyUID, seriesUID, instanceUID, func(p Part) error {
		ds, err := p.Dataset()
		if err != nil {
			return err
		}
		out = append(out, ds)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RetrieveMetadata returns the instances of a study, a series or one
// instance as data sets without their bulk data, which is how a study is
// examined without downloading it. Pixel Data and other large values come
// back empty.
func (c *Client) RetrieveMetadata(ctx context.Context, studyUID, seriesUID, instanceUID string) ([]*dataset.Dataset, error) {
	segments, err := instancePath(studyUID, seriesUID, instanceUID)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, http.MethodGet, c.resource(append(segments, "metadata")...), jsonMedia, "", nil,
		http.StatusOK, http.StatusNoContent)
	if err != nil {
		return nil, err
	}
	return c.jsonDatasets(response)
}

// instancePath is the resource for a study, one of its series, or one of
// that series' instances.
func instancePath(studyUID, seriesUID, instanceUID string) ([]string, error) {
	switch {
	case studyUID == "":
		return nil, fmt.Errorf("dicomweb: no Study Instance UID given")
	case seriesUID == "" && instanceUID != "":
		return nil, fmt.Errorf("dicomweb: instance %s is named without its series", instanceUID)
	case seriesUID == "":
		return []string{"studies", studyUID}, nil
	case instanceUID == "":
		return []string{"studies", studyUID, "series", seriesUID}, nil
	}
	return []string{"studies", studyUID, "series", seriesUID, "instances", instanceUID}, nil
}
