package dcmstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/fileutil"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// indexFileName holds the index between runs, so a restart does not have to read
// every file to answer a query.
const indexFileName = "index.json"

// transferSyntaxUID is what instances are written as.
//
// Explicit VR Little Endian, always, whatever the instance arrived as: a store
// that keeps each instance in the syntax it was received in has to record which
// that was per file, and the point of this package is to be simple enough to
// trust. Pixel data is not recompressed — a compressed instance keeps its
// encapsulated bytes, and only the surrounding data set is re-encoded.
const transferSyntaxUID = "1.2.840.10008.1.2.1"

// Instance is one stored SOP instance and the attributes it can be queried by.
type Instance struct {
	SOPClassUID    string `json:"sopClassUID"`
	SOPInstanceUID string `json:"sopInstanceUID"`

	// Path is relative to the store's root, so the store survives being moved.
	Path string `json:"path"`

	// Keys holds the query attributes, by tag, with the VR each was stored with.
	// The VR decides which matching rule applies, so it has to be kept rather
	// than looked up: a private or non-conformant element may not have the VR the
	// dictionary expects, and matching it by the dictionary's would be wrong.
	Keys map[string]IndexedValue `json:"keys"`
}

// IndexedValue is one indexed attribute value.
type IndexedValue struct {
	VR    string `json:"vr"`
	Value string `json:"value"`
}

// value returns an indexed attribute's value alone.
func (i *Instance) value(t tag.Tag) string {
	return i.Keys[t.String()].Value
}

// Store is an on-disk collection of DICOM instances with an index that can
// answer the hierarchical queries a C-FIND asks.
//
// It is safe for concurrent use: an SCP handles each association in its own
// goroutine, so a store behind one is written and read concurrently by
// construction.
type Store struct {
	root string

	mu        sync.RWMutex
	instances map[string]*Instance // by SOP Instance UID
}

// Open returns the store rooted at dir, creating the directory if it does not
// exist.
//
// The index is loaded from dir if it is there. If it is missing or unreadable the
// tree is walked and the index rebuilt, so a store remains usable after the index
// is lost — losing an index should cost a slow start, not the archive.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("the store directory is empty")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating the store directory %q: %w", dir, err)
	}

	s := &Store{root: dir, instances: make(map[string]*Instance)}

	if err := s.loadIndex(); err == nil {
		return s, nil
	}

	// No usable index. Rebuild from the files themselves.
	if err := s.Rebuild(context.Background()); err != nil {
		return nil, fmt.Errorf("rebuilding the index for %q: %w", dir, err)
	}
	return s, nil
}

// Root returns the directory the store is rooted at.
func (s *Store) Root() string { return s.root }

// Count returns how many instances the store holds.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.instances)
}

// Store writes an instance and indexes it, returning what was stored.
//
// The data set must carry SOP Class UID (0008,0016) and SOP Instance UID
// (0008,0018): the first says what it is and the second names it, and a store
// cannot file an instance without either. Storing the same SOP Instance UID twice
// replaces the first, which is what a repeated C-STORE of one instance means.
func (s *Store) Store(ctx context.Context, ds *dataset.Dataset) (*Instance, error) {
	if ds == nil {
		return nil, fmt.Errorf("the data set is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sopClassUID := stringValue(ds, tagSOPClassUID)
	sopInstanceUID := stringValue(ds, tagSOPInstanceUID)
	if sopClassUID == "" {
		return nil, fmt.Errorf("the data set has no SOP Class UID (0008,0016)")
	}
	if sopInstanceUID == "" {
		return nil, fmt.Errorf("the data set has no SOP Instance UID (0008,0018)")
	}

	// The UID names a file, and it came from a peer. fileutil.InstanceFilePath
	// refuses anything that is not dotted decimal, so a UID carrying "../" cannot
	// place the file outside the store.
	dir, err := s.instanceDir(ds)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating %q: %w", dir, err)
	}
	path, err := fileutil.InstanceFilePath(dir, sopInstanceUID)
	if err != nil {
		return nil, err
	}

	if err := writeInstance(path, sopClassUID, sopInstanceUID, ds); err != nil {
		return nil, err
	}

	relative, err := filepath.Rel(s.root, path)
	if err != nil {
		return nil, fmt.Errorf("locating %q within %q: %w", path, s.root, err)
	}

	inst := &Instance{
		SOPClassUID:    sopClassUID,
		SOPInstanceUID: sopInstanceUID,
		Path:           filepath.ToSlash(relative),
		Keys:           indexKeys(ds),
	}

	s.mu.Lock()
	s.instances[sopInstanceUID] = inst
	s.mu.Unlock()

	// A failure to save the index is not a failure to store: the instance is on
	// disk and Rebuild will find it. Reporting it would have the SCP refuse an
	// instance it has in fact kept.
	if err := s.saveIndex(); err != nil {
		storeLogger.Warn("dcmstore: could not save the index, which will be rebuilt on the next Open: %v", err)
	}

	return inst, nil
}

// instanceDir returns the directory an instance belongs in, study over series, so
// that the tree mirrors the hierarchy a query walks.
//
// Both UIDs are validated as path components. An instance missing either is
// filed directly under the root rather than refused — the UIDs are type 1 for
// every composite instance, but a store that drops a non-conformant instance
// loses data an archive would have kept.
func (s *Store) instanceDir(ds *dataset.Dataset) (string, error) {
	dir := s.root

	for _, t := range []tag.Tag{tagStudyInstanceUID, tagSeriesInstanceUID} {
		value := stringValue(ds, t)
		if value == "" {
			continue
		}
		if err := fileutil.ValidateUIDForPath(value); err != nil {
			return "", fmt.Errorf("%s cannot be part of a path: %w", t.String(), err)
		}
		dir = filepath.Join(dir, value)
	}

	return dir, nil
}

// Instance returns a stored instance by its SOP Instance UID.
func (s *Store) Instance(sopInstanceUID string) (*Instance, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inst, ok := s.instances[sopInstanceUID]
	return inst, ok
}

// Instances returns every stored instance, ordered by SOP Instance UID so that
// two calls agree.
func (s *Store) Instances() []*Instance {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Instance, 0, len(s.instances))
	for _, inst := range s.instances {
		out = append(out, inst)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SOPInstanceUID < out[j].SOPInstanceUID
	})
	return out
}

// Load reads an instance back off disk.
//
// The data set is read fresh each time rather than cached: a store holding every
// instance in memory would not survive an archive of any size, and the index is
// what makes that unnecessary.
func (s *Store) Load(ctx context.Context, inst *Instance) (*dataset.Dataset, error) {
	if inst == nil {
		return nil, fmt.Errorf("the instance is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path := filepath.Join(s.root, filepath.FromSlash(inst.Path))
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", inst.SOPInstanceUID, err)
	}
	defer func() { _ = file.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(file))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", inst.SOPInstanceUID, err)
	}

	return df.GetDataset(), nil
}

// Remove deletes an instance from the store and its index.
func (s *Store) Remove(sopInstanceUID string) error {
	s.mu.Lock()
	inst, ok := s.instances[sopInstanceUID]
	if ok {
		delete(s.instances, sopInstanceUID)
	}
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("no instance %q in the store", sopInstanceUID)
	}

	path := filepath.Join(s.root, filepath.FromSlash(inst.Path))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", path, err)
	}

	if err := s.saveIndex(); err != nil {
		storeLogger.Warn("dcmstore: could not save the index after a removal: %v", err)
	}
	return nil
}

// Rebuild discards the index and reads it back from the files in the store.
//
// A file that cannot be read is skipped with a warning rather than failing the
// rebuild: one corrupt instance should not make the rest of an archive
// unqueryable.
func (s *Store) Rebuild(ctx context.Context) error {
	rebuilt := make(map[string]*Instance)
	skipped := 0

	err := filepath.WalkDir(s.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".dcm") {
			return nil
		}

		inst, readErr := s.readInstance(path)
		if readErr != nil {
			skipped++
			storeLogger.Warn("dcmstore: skipping %s during the rebuild: %v", path, readErr)
			return nil
		}
		rebuilt[inst.SOPInstanceUID] = inst
		return nil
	})
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.instances = rebuilt
	s.mu.Unlock()

	if skipped > 0 {
		storeLogger.Warn("dcmstore: rebuilt the index of %s with %d instances, skipping %d unreadable file(s)",
			s.root, len(rebuilt), skipped)
	}

	return s.saveIndex()
}

// readInstance reads one file and derives its index entry.
func (s *Store) readInstance(path string) (*Instance, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(file))
	if err != nil {
		return nil, err
	}
	ds := df.GetDataset()

	sopClassUID := stringValue(ds, tagSOPClassUID)
	sopInstanceUID := stringValue(ds, tagSOPInstanceUID)
	if sopInstanceUID == "" {
		// Fall back to the file meta header, which carries the same UID and may
		// have survived where the data set element did not.
		if df.FileMetaInfo != nil {
			sopInstanceUID = df.FileMetaInfo.MediaStorageSOPInstanceUID
			if sopClassUID == "" {
				sopClassUID = df.FileMetaInfo.MediaStorageSOPClassUID
			}
		}
	}
	if sopInstanceUID == "" {
		return nil, fmt.Errorf("no SOP Instance UID in the data set or the file meta header")
	}

	relative, err := filepath.Rel(s.root, path)
	if err != nil {
		return nil, err
	}

	return &Instance{
		SOPClassUID:    sopClassUID,
		SOPInstanceUID: sopInstanceUID,
		Path:           filepath.ToSlash(relative),
		Keys:           indexKeys(ds),
	}, nil
}

// loadIndex reads the index file.
func (s *Store) loadIndex() error {
	data, err := os.ReadFile(filepath.Join(s.root, indexFileName))
	if err != nil {
		return err
	}

	var loaded map[string]*Instance
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	if loaded == nil {
		loaded = make(map[string]*Instance)
	}

	s.mu.Lock()
	s.instances = loaded
	s.mu.Unlock()
	return nil
}

// saveIndex writes the index file.
//
// Written to a temporary file and renamed, so an interrupted save leaves the
// previous index rather than a truncated one. A truncated index would be read
// back as a shorter archive, which is worse than no index at all — that at least
// triggers a rebuild.
func (s *Store) saveIndex() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.instances, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("encoding the index: %w", err)
	}

	final := filepath.Join(s.root, indexFileName)
	temporary, err := os.CreateTemp(s.root, indexFileName+".*")
	if err != nil {
		return fmt.Errorf("creating a temporary index file: %w", err)
	}
	name := temporary.Name()

	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		_ = os.Remove(name)
		return fmt.Errorf("writing the index: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("closing the index: %w", err)
	}
	if err := os.Rename(name, final); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("replacing the index: %w", err)
	}
	return nil
}

// writeInstance writes a data set as a Part 10 file.
func writeInstance(path, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %q: %w", path, err)
	}

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(file))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    sopClassUID,
		MediaStorageSOPInstanceUID: sopInstanceUID,
		TransferSyntaxUID:          transferSyntaxUID,
	})

	// ElementsFromDataset descends into sequences. Copying Value and ignoring
	// Items writes a file that looks complete with every nested item missing.
	for _, elem := range filewriter.ElementsFromDataset(ds) {
		if err := w.AddDataElement(elem); err != nil {
			_ = w.Close()
			_ = os.Remove(path)
			return fmt.Errorf("adding %s: %w", elem.Tag, err)
		}
	}

	if err := w.Write(); err != nil {
		_ = w.Close()
		// A half-written file would be found by Rebuild and read as an instance.
		_ = os.Remove(path)
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return w.Close()
}

// stringValue reads an element's value as the string form matching uses.
func stringValue(ds *dataset.Dataset, t tag.Tag) string {
	if ds == nil {
		return ""
	}
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	return elementString(elem)
}

// elementString renders an element's value as a string.
//
// Values reach a data set as []byte from a reader and as string, or a converted
// type, from code that built one by hand. Both have to read the same way here, or
// an instance stored from the network would index differently from the same
// instance read off disk.
func elementString(elem *dataelem.DataElement) string {
	if elem == nil {
		return ""
	}
	switch v := elem.GetValue().(type) {
	case nil:
		return ""
	case string:
		return trimPadding(v)
	case []byte:
		return trimPadding(string(v))
	case []string:
		return trimPadding(strings.Join(v, `\`))
	default:
		return trimPadding(fmt.Sprintf("%v", v))
	}
}
