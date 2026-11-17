package dcmstore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
)

// What a DICOMweb server needs of an archive beyond Query: which instances
// it holds of a study, and each one's file. With these a Store is a
// dicomweb.Backend as it is. They are written against the standard library
// alone, so this package does not depend on that one.

// Locate names the instances held of a study: all of them, those of one
// series when series is given, or the one instance when instance is given
// too. It returns their SOP Instance UIDs in the order a query returns them,
// and none when the store holds nothing of what was named.
func (s *Store) Locate(ctx context.Context, study, series, instance string) ([]string, error) {
	if study == "" {
		return nil, fmt.Errorf("no Study Instance UID given")
	}
	query := dataset.NewDataset()
	_ = query.Add(dataelem.NewDataElement(tagQueryRetrieveLevel, dataelem.CS, string(LevelImage)))
	_ = query.Add(dataelem.NewDataElement(tagStudyInstanceUID, dataelem.UI, study))
	if series != "" {
		_ = query.Add(dataelem.NewDataElement(tagSeriesInstanceUID, dataelem.UI, series))
	}
	if instance != "" {
		_ = query.Add(dataelem.NewDataElement(tagSOPInstanceUID, dataelem.UI, instance))
	}
	matched, err := s.matchInstances(ctx, query, LevelImage)
	if err != nil {
		return nil, err
	}
	uids := make([]string, len(matched))
	for i, inst := range matched {
		uids[i] = inst.SOPInstanceUID
	}
	return uids, nil
}

// Open returns the Part 10 file of a stored instance, as it is on disk. The
// caller closes it.
func (s *Store) Open(_ context.Context, sopInstanceUID string) (io.ReadSeekCloser, error) {
	inst, ok := s.Instance(sopInstanceUID)
	if !ok {
		return nil, fmt.Errorf("the store holds no instance %s", sopInstanceUID)
	}
	// The path is the index's own, made from UIDs checked when it was stored.
	file, err := os.Open(filepath.Join(s.root, filepath.FromSlash(inst.Path)))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", sopInstanceUID, err)
	}
	return file, nil
}

// Put keeps an instance, as Store does, for a caller that has no use for
// the index entry.
func (s *Store) Put(ctx context.Context, instance *dataset.Dataset) error {
	_, err := s.Store(ctx, instance)
	return err
}
