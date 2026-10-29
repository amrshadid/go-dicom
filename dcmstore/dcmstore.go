package dcmstore

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
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

	// Sequences holds the indexed sequences, by sequence tag, one map of
	// attributes per item. Only the sequences in indexedSequences are here —
	// indexing every nested attribute of every instance would put the whole data
	// set back in memory, which is what writing the files was meant to avoid.
	//
	// Omitted from the index file when empty, which is the common case: most
	// instances carry none of them.
	Sequences map[string][]map[string]IndexedValue `json:"sequences,omitempty"`
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

	// ops is held shared by Store and Remove and exclusively by Rebuild, which
	// replaces the index with what it read from disk and so cannot have
	// either change the store under it.
	ops sync.RWMutex
	// uidLocks serialize the writers of one SOP Instance UID, so its file and
	// its index entry are always the same copy.
	uidLocks [64]sync.Mutex
	// saving serializes saveIndex, whose snapshot must be the one renamed in.
	saving sync.Mutex
}

// lockUID locks the writers of one SOP Instance UID and returns the unlock.
func (s *Store) lockUID(uid string) func() {
	h := fnv.New32a()
	_, _ = h.Write([]byte(uid))
	m := &s.uidLocks[h.Sum32()%uint32(len(s.uidLocks))]
	m.Lock()
	return m.Unlock
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

	s.ops.RLock()
	defer s.ops.RUnlock()
	defer s.lockUID(sopInstanceUID)()

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
		Sequences:      indexSequences(ds),
	}

	s.mu.Lock()
	previous := s.instances[sopInstanceUID]
	s.instances[sopInstanceUID] = inst
	s.mu.Unlock()

	// Stored again under another study or series, the instance has a new path.
	// The old file would otherwise stay, and Rebuild would find it: a removed
	// instance came back that way, as did an older copy of a replaced one.
	if previous != nil && previous.Path != inst.Path {
		old := filepath.Join(s.root, filepath.FromSlash(previous.Path))
		if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
			storeLogger.Warn("dcmstore: could not remove the replaced copy %s: %v", old, err)
		}
	}

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
	s.ops.RLock()
	defer s.ops.RUnlock()
	defer s.lockUID(sopInstanceUID)()

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
	s.ops.Lock()
	defer s.ops.Unlock()

	rebuilt := make(map[string]*Instance)
	modified := make(map[string]int64) // by SOP Instance UID, for duplicates
	skipped := 0

	err := filepath.WalkDir(s.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		// What a store interrupted mid-write leaves beside the file it was
		// replacing. Rebuild holds the store exclusively, so none is in use.
		if name := entry.Name(); strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".tmp") {
			if err := os.Remove(path); err != nil {
				storeLogger.Warn("dcmstore: could not remove the interrupted write %s: %v", path, err)
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".dcm") {
			return nil
		}

		inst, readErr := s.readInstance(path)
		if readErr != nil {
			skipped++
			storeLogger.Warn("dcmstore: skipping %s during the rebuild: %v", path, readErr)
			return nil
		}
		// Two files for one UID can only be left by a version that did not
		// remove the old copy when an instance moved. The newer is the one the
		// last C-STORE wrote.
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		stamp := info.ModTime().UnixNano()
		if _, seen := rebuilt[inst.SOPInstanceUID]; seen {
			storeLogger.Warn("dcmstore: %s has more than one file; keeping the newer", inst.SOPInstanceUID)
			if stamp < modified[inst.SOPInstanceUID] {
				return nil
			}
		}
		rebuilt[inst.SOPInstanceUID] = inst
		modified[inst.SOPInstanceUID] = stamp
		return nil
	})
	if err != nil {
		return err
	}

	count := len(rebuilt)
	s.mu.Lock()
	s.instances = rebuilt
	s.mu.Unlock()

	if skipped > 0 {
		storeLogger.Warn("dcmstore: rebuilt the index of %s with %d instances, skipping %d unreadable file(s)",
			s.root, count, skipped)
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
		Sequences:      indexSequences(ds),
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
	// The snapshot and the rename happen under one lock. Taken apart, an older
	// snapshot could be renamed in after a newer one, and the index on disk
	// would lack an instance the C-STORE had acknowledged.
	s.saving.Lock()
	defer s.saving.Unlock()

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
//
// Uncompressed instances are written as Explicit VR Little Endian, whatever
// they arrived as, and compressed ones in the syntax they arrived in, which is
// the only record of their codec; filewriter.StorageTransferSyntax decides. Pixel
// data is never recompressed or decoded on the way in. Every instance was
// written as Explicit VR Little Endian once, compressed ones included, and those
// files hold fragments no reader can decode.
//
// The file is written beside its final path and renamed into place once it is
// complete, so a reader, Rebuild, or a crash never sees part of one. Writing
// into the final path truncated the previous copy first: a store killed midway
// left an empty or partial file where a good one had been.
func writeInstance(path, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) error {
	// Not ending in .dcm, so Rebuild passes over one a crash leaves behind.
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating a file beside %q: %w", path, err)
	}
	temporary := file.Name()
	if err := writePart10(file, temporary, sopClassUID, sopInstanceUID, ds); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("moving %q into place: %w", path, err)
	}
	return nil
}

// writePart10 writes the file and syncs it, so the rename that follows never
// puts a file in place whose contents are not yet on disk.
func writePart10(file *os.File, path, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) error {

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(file))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    sopClassUID,
		MediaStorageSOPInstanceUID: sopInstanceUID,
		TransferSyntaxUID:          filewriter.StorageTransferSyntax(ds),
	})

	// ElementsFromDataset descends into sequences. Copying Value and ignoring
	// Items writes a file that looks complete with every nested item missing.
	for _, elem := range filewriter.ElementsFromDataset(ds) {
		if err := w.AddDataElement(elem); err != nil {
			_ = w.Close()
			return fmt.Errorf("adding %s: %w", elem.Tag, err)
		}
	}

	if err := w.Write(); err != nil {
		_ = w.Close()
		return fmt.Errorf("writing %q: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		_ = w.Close()
		return fmt.Errorf("syncing %q: %w", path, err)
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
