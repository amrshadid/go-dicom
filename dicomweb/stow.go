package dicomweb

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// StoreResult is a server's account of a STOW-RS request (PS3.18 10.5.3).
type StoreResult struct {
	// RetrieveURL is where the study can be retrieved from, when the server
	// says.
	RetrieveURL string
	// Stored are the instances the server kept.
	Stored []StoredInstance
	// Failed are the ones it did not.
	Failed []FailedInstance
}

// StoredInstance is an instance a server kept.
type StoredInstance struct {
	SOPClassUID, SOPInstanceUID string
	RetrieveURL                 string
	// WarningReason is nonzero when the server kept the instance but
	// changed it: coerced attributes (B000H), dropped elements (B006H) or
	// found it not to match its SOP class (B007H).
	WarningReason uint16
}

// FailedInstance is an instance a server did not keep.
type FailedInstance struct {
	SOPClassUID, SOPInstanceUID string
	// FailureReason is the server's status for it (PS3.18 Table 10.5.3-1),
	// such as 0110H processing failure or 0122H SOP class not supported.
	FailureReason uint16
}

// Store sends DICOM Part 10 files to the service, into the study when
// studyUID is given, and returns what the server says became of each.
//
// A server that keeps some answers 202, which is not an error: Failed says
// which were not kept. One that keeps none answers 409, and some servers
// answer 400 for either. A refusal is returned as a *StatusError, together
// with the result whenever the server gave its account of each instance.
func (c *Client) Store(ctx context.Context, studyUID string, files ...[]byte) (*StoreResult, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("dicomweb: nothing to store")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for i, file := range files {
		part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/dicom"}})
		if err != nil {
			return nil, fmt.Errorf("dicomweb: %w", err)
		}
		if len(file) == 0 {
			return nil, fmt.Errorf("dicomweb: file %d is empty", i)
		}
		if _, err := part.Write(file); err != nil {
			return nil, fmt.Errorf("dicomweb: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("dicomweb: %w", err)
	}

	segments := []string{"studies"}
	if studyUID != "" {
		segments = append(segments, studyUID)
	}
	contentType := `multipart/related; type="application/dicom"; boundary=` + writer.Boundary()
	u := c.resource(segments...)
	response, err := c.send(ctx, http.MethodPost, u, jsonMedia, contentType, &body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	// Read whole whatever the status: a refusal carries the same account as
	// a success does, and with many instances it is long.
	data, err := io.ReadAll(c.limited(response.Body))
	if err != nil {
		return nil, fmt.Errorf("dicomweb: reading the response: %w", err)
	}
	var result *StoreResult
	ds := dataset.NewDataset()
	if len(bytes.TrimSpace(data)) > 0 && ds.FromDICOMJSONString(string(data)) == nil {
		result = storeResult(ds)
	}

	switch response.StatusCode {
	case http.StatusOK, http.StatusAccepted:
		if result == nil {
			if len(bytes.TrimSpace(data)) > 0 {
				return nil, fmt.Errorf("dicomweb: the store response is not DICOM JSON")
			}
			// Stored, and the server did not say more.
			result = &StoreResult{}
		}
		return result, nil
	}
	// A body that is not the account of any instance is only an explanation.
	if result != nil && len(result.Stored)+len(result.Failed) == 0 {
		result = nil
	}
	return result, statusError(u, response, data)
}

// StoreDatasets is Store for data sets, each written as a Part 10 file in
// the transfer syntax it holds: a compressed image is sent as it is.
func (c *Client) StoreDatasets(ctx context.Context, studyUID string, instances ...*dataset.Dataset) (*StoreResult, error) {
	files := make([][]byte, 0, len(instances))
	for i, ds := range instances {
		file, err := encodeFile(ds)
		if err != nil {
			return nil, fmt.Errorf("dicomweb: instance %d: %w", i, err)
		}
		files = append(files, file)
	}
	return c.Store(ctx, studyUID, files...)
}

var (
	tagRetrieveURL    = tag.New(0x0008, 0x1190)
	tagReferencedSOPs = tag.New(0x0008, 0x1199)
	tagFailedSOPs     = tag.New(0x0008, 0x1198)
	tagRefSOPClass    = tag.New(0x0008, 0x1150)
	tagRefSOPInstance = tag.New(0x0008, 0x1155)
	tagFailureReason  = tag.New(0x0008, 0x1197)
	tagWarningReason  = tag.New(0x0008, 0x1196)
)

func storeResult(ds *dataset.Dataset) *StoreResult {
	result := &StoreResult{RetrieveURL: text(ds, tagRetrieveURL)}
	for _, item := range items(ds, tagReferencedSOPs) {
		result.Stored = append(result.Stored, StoredInstance{
			SOPClassUID:    text(item, tagRefSOPClass),
			SOPInstanceUID: text(item, tagRefSOPInstance),
			RetrieveURL:    text(item, tagRetrieveURL),
			WarningReason:  number(item, tagWarningReason),
		})
	}
	for _, item := range items(ds, tagFailedSOPs) {
		result.Failed = append(result.Failed, FailedInstance{
			SOPClassUID:    text(item, tagRefSOPClass),
			SOPInstanceUID: text(item, tagRefSOPInstance),
			FailureReason:  number(item, tagFailureReason),
		})
	}
	return result
}

// items returns the data sets of a sequence.
func items(ds *dataset.Dataset, t tag.Tag) []*dataset.Dataset {
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	seq, ok := elem.GetValue().(*sequence.Sequence)
	if !ok {
		return nil
	}
	var out []*dataset.Dataset
	for _, item := range seq.Items() {
		if inner, ok := item.(*dataset.Dataset); ok {
			out = append(out, inner)
		}
	}
	return out
}

// text reads a text value without its padding.
func text(ds *dataset.Dataset, t tag.Tag) string {
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	switch v := elem.GetValue().(type) {
	case []byte:
		return strings.TrimRight(string(v), " \x00")
	case string:
		return strings.TrimRight(v, " \x00")
	}
	return ""
}

// number reads an unsigned short, however a JSON-built data set holds it.
func number(ds *dataset.Dataset, t tag.Tag) uint16 {
	elem, ok := ds.Get(t)
	if !ok {
		return 0
	}
	switch v := elem.GetValue().(type) {
	case []byte:
		if len(v) >= 2 {
			return binary.LittleEndian.Uint16(v)
		}
	case uint16:
		return v
	case int:
		return uint16(v)
	case json.Number:
		n, _ := strconv.ParseUint(v.String(), 10, 16)
		return uint16(n)
	case string:
		n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 16)
		return uint16(n)
	}
	return 0
}

// encodeFile writes a data set as a DICOM Part 10 file.
func encodeFile(ds *dataset.Dataset) ([]byte, error) {
	if ds == nil {
		return nil, fmt.Errorf("no data set")
	}
	sopClass, sopInstance := text(ds, tag.New(0x0008, 0x0016)), text(ds, tag.New(0x0008, 0x0018))
	if sopClass == "" || sopInstance == "" {
		return nil, fmt.Errorf("the data set has no SOP Class UID or SOP Instance UID, so it is not an instance")
	}
	out := &memoryFile{}
	writer := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	writer.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    sopClass,
		MediaStorageSOPInstanceUID: sopInstance,
		TransferSyntaxUID:          filewriter.StorageTransferSyntax(ds),
	})
	if err := writer.AddDataElements(filewriter.ElementsFromDataset(ds)); err != nil {
		return nil, err
	}
	if err := writer.Write(); err != nil {
		return nil, err
	}
	return out.data, nil
}

// memoryFile is an in-memory io.WriteSeeker, which the file writer needs to
// go back and fill in the group length it could not know up front.
type memoryFile struct {
	data []byte
	pos  int64
}

func (m *memoryFile) Write(p []byte) (int, error) {
	end := m.pos + int64(len(p))
	if end > int64(len(m.data)) {
		grown := make([]byte, end)
		copy(grown, m.data)
		m.data = grown
	}
	copy(m.data[m.pos:], p)
	m.pos = end
	return len(p), nil
}

func (m *memoryFile) Seek(offset int64, whence int) (int64, error) {
	next := offset
	switch whence {
	case io.SeekCurrent:
		next = m.pos + offset
	case io.SeekEnd:
		next = int64(len(m.data)) + offset
	}
	if next < 0 {
		return 0, fmt.Errorf("seek to %d, before the start", next)
	}
	m.pos = next
	return next, nil
}
