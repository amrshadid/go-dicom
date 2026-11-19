package dcmstore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// The index on disk is two files: index.json, a snapshot of every
// instance, and index.log, what has changed since, one JSON record a line,
// appended. Opening reads the snapshot and replays the log; a store appends
// a record, not the archive. When the log has grown as long as the archive
// it is folded into a new snapshot and begun again, so replaying it never
// costs more than reading the snapshot does, and a store costs the same at
// the hundred-thousandth instance as at the first.
//
// Rewriting the whole index on every store, which this replaces, cost a
// store time in proportion to the archive: 11 ms an instance over the first
// thousand and 865 ms over the hundredth, when the index had grown to 108 MB
// (#410, measured by TestMeasureIndexCost). With the journal both are
// the cost of writing and syncing the instance's file.
//
// A store is two steps that cannot be made one: the file is renamed into
// place, and the index says so. A crash between them must not lose an
// instance that was written, nor keep one that was not. So before the
// rename a "begin" record naming the instance and its path is appended and
// synced, and after it a "put" record carries the index entry. A begin left
// without its put is settled at the next Open from the disk itself: the
// file at that path is indexed if it is there and is the instance, and the
// entry dropped if it is not. A removal is the same, begun, the file
// deleted, and ended with a "remove" record.

// journalFileName holds what has changed since the snapshot.
const journalFileName = "index.log"

// The kinds of record in the journal.
const (
	recordBegin  = "begin"
	recordPut    = "put"
	recordRemove = "remove"
)

// journalRecord is one line of the journal.
type journalRecord struct {
	// Seq ties a put or a remove to the begin it ends.
	Seq uint64 `json:"seq"`
	Op  string `json:"op"`
	// UID and Path are the instance a begin or a remove is about, and
	// where its file is.
	UID  string `json:"uid,omitempty"`
	Path string `json:"path,omitempty"`
	// Instance is the entry a put records.
	Instance *Instance `json:"instance,omitempty"`
}

// compactAfter is the fewest records the journal holds before it may be
// folded into the snapshot; beyond it, the journal is folded once it holds
// as many records as the archive has instances.
var compactAfter = 4096

// appendJournal adds a record to the journal. sync makes it durable before
// returning, which a begin needs: it is what lets a crash after the rename
// be found. A put or a remove that is lost leaves its begin, which the next
// Open settles from the disk.
//
// The file is opened for each record and closed after, so a store holds no
// file open between calls: Windows will not remove a directory with an
// open file in it, and a Store has no Close.
func (s *Store) appendJournal(record journalRecord, sync bool) (uint64, error) {
	s.journal.Lock()
	defer s.journal.Unlock()
	if record.Op == recordBegin {
		s.seq++
		record.Seq = s.seq
	}
	line, err := json.Marshal(record)
	if err != nil {
		return 0, fmt.Errorf("encoding an index record: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(s.root, journalFileName), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return 0, fmt.Errorf("opening the index journal: %w", err)
	}
	_, err = file.Write(append(line, '\n'))
	if err == nil && sync {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, fmt.Errorf("appending to the index journal: %w", err)
	}
	s.records++
	return record.Seq, nil
}

// replayJournal applies the journal to the instances the snapshot holds,
// and settles from the disk each change that was begun and not ended.
//
// A last line without its newline is a record a crash cut short. It is
// dropped, and cut from the file, so the next record does not run on from
// it. Any other line that does not read is an error, which Open answers by
// rebuilding from the files.
func (s *Store) replayJournal(instances map[string]*Instance) error {
	path := filepath.Join(s.root, journalFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	whole := len(data)
	if end := bytes.LastIndexByte(data, '\n'); end+1 != len(data) {
		whole = end + 1
		storeLogger.Warn("dcmstore: the index journal ends in a record a crash cut short; it is dropped")
	}

	open := map[uint64]journalRecord{}
	reader := bufio.NewReader(bytes.NewReader(data[:whole]))
	for line := 1; ; line++ {
		text, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		var record journalRecord
		if err := json.Unmarshal(text, &record); err != nil {
			return fmt.Errorf("the index journal, line %d: %w", line, err)
		}
		switch record.Op {
		case recordBegin:
			open[record.Seq] = record
		case recordPut:
			if record.Instance == nil {
				return fmt.Errorf("the index journal, line %d: a put with no instance", line)
			}
			instances[record.Instance.SOPInstanceUID] = record.Instance
			delete(open, record.Seq)
		case recordRemove:
			delete(instances, record.UID)
			delete(open, record.Seq)
		default:
			return fmt.Errorf("the index journal, line %d: a record of kind %q", line, record.Op)
		}
		s.records++
		s.seq = max(s.seq, record.Seq)
	}

	// Settled in the order they were begun, so that of two unfinished
	// changes to one instance the later decides.
	for seq := uint64(1); seq <= s.seq && len(open) > 0; seq++ {
		record, ok := open[seq]
		if !ok {
			continue
		}
		delete(open, seq)
		s.settle(instances, record)
	}

	if whole != len(data) {
		if err := os.Truncate(path, int64(whole)); err != nil {
			return fmt.Errorf("cutting the unfinished record from the index journal: %w", err)
		}
	}
	return nil
}

// settle decides a change that was begun and not ended from what is on
// disk: the file at the path it named is the instance if it is there and
// says so, and otherwise the instance is not at that path.
func (s *Store) settle(instances map[string]*Instance, record journalRecord) {
	full := filepath.Join(s.root, filepath.FromSlash(record.Path))
	inst, err := s.readInstance(full)
	switch {
	case err == nil && inst.SOPInstanceUID == record.UID:
		instances[record.UID] = inst
		storeLogger.Warn("dcmstore: %s was written and not indexed before the store stopped; it is indexed now", record.UID)
	case errors.Is(err, os.ErrNotExist):
		if held, ok := instances[record.UID]; ok && held.Path == record.Path {
			delete(instances, record.UID)
		}
	case err != nil:
		storeLogger.Warn("dcmstore: %s at %s could not be read after the store stopped: %v", record.UID, record.Path, err)
		if held, ok := instances[record.UID]; ok && held.Path == record.Path {
			delete(instances, record.UID)
		}
	}
}

// compactIfLong folds the journal into a new snapshot once it holds more
// records than the archive has instances, and compactAfter at the least.
// It takes the store exclusively, as Rebuild does, so no change is between
// the snapshot and the journal it replaces.
func (s *Store) compactIfLong() {
	s.journal.Lock()
	long := s.records > compactAfter && s.records > s.Count()
	s.journal.Unlock()
	if !long {
		return
	}
	s.ops.Lock()
	defer s.ops.Unlock()
	if err := s.writeSnapshot(); err != nil {
		storeLogger.Warn("dcmstore: could not fold the index journal into the index: %v", err)
	}
}

// writeSnapshot writes the index file and begins the journal again. The
// caller holds ops exclusively.
//
// The snapshot is written to a temporary file, synced and renamed, so an
// interrupted write leaves the previous one. The journal is removed only
// after: a crash between the two replays the old journal onto the new
// snapshot, which holds everything the journal does, and comes to the same
// instances.
func (s *Store) writeSnapshot() error {
	s.mu.RLock()
	data, err := json.Marshal(indexFile{Version: indexVersion, Instances: s.instances})
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
	_, err = temporary.Write(data)
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, final)
	}
	if err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("writing the index: %w", err)
	}

	s.journal.Lock()
	defer s.journal.Unlock()
	if err := os.Remove(filepath.Join(s.root, journalFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("beginning the index journal again: %w", err)
	}
	s.records = 0
	return nil
}
