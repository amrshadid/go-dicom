package dcmstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// The index is a snapshot and a journal of the changes since. These tests
// look at the files themselves, and stop a store between its steps the way
// a crash would, by doing the first steps and not the rest.

// reopen opens the store at dir again, as a restart would.
func reopen(t *testing.T, dir string) *Store {
	t.Helper()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}

// storeN stores generated instances from to to, not including to.
func storeN(t *testing.T, store *Store, from, to int) {
	t.Helper()
	for n := from; n < to; n++ {
		if _, err := store.Store(context.Background(), generated(n)); err != nil {
			t.Fatalf("Store(%d): %v", n, err)
		}
	}
}

// uidOf is the SOP Instance UID of generated instance n.
func uidOf(n int) string { return stringValue(generated(n), tagSOPInstanceUID) }

// held lists what a store holds: each instance's UID and path.
func held(store *Store) []string {
	var out []string
	for _, inst := range store.Instances() {
		out = append(out, inst.SOPInstanceUID+" "+inst.Path)
	}
	sort.Strings(out)
	return out
}

// TestAStoreAppendsAndDoesNotRewriteTheIndex checks that storing writes a
// record to the journal and leaves the snapshot alone, so a store costs the
// same whatever the archive holds; and that the journal is read back.
func TestAStoreAppendsAndDoesNotRewriteTheIndex(t *testing.T) {
	dir := t.TempDir()
	store := reopen(t, dir)
	snapshot, err := os.ReadFile(filepath.Join(dir, indexFileName))
	if err != nil {
		t.Fatal(err)
	}
	storeN(t, store, 0, 200)

	after, err := os.ReadFile(filepath.Join(dir, indexFileName))
	if err != nil || !bytes.Equal(after, snapshot) {
		t.Fatalf("the index file was rewritten by a store (%d bytes, then %d)", len(snapshot), len(after))
	}
	journal, err := os.ReadFile(filepath.Join(dir, journalFileName))
	if err != nil {
		t.Fatal(err)
	}
	if lines := bytes.Count(journal, []byte("\n")); lines != 400 {
		t.Errorf("the journal holds %d records for 200 stores, want a begin and a put each", lines)
	}
	if got := reopen(t, dir); !equal(held(got), held(store)) {
		t.Errorf("reopened, the store holds %d instances, want %d", got.Count(), store.Count())
	}
}

// TestACrashBetweenFileAndIndexIsSettled stops a store, and a removal,
// after its begin record and at each step after, and checks that the next
// Open holds what is on disk: a file written and not indexed is indexed, a
// begin whose file never arrived adds nothing, and a removal whose file is
// gone is removed.
func TestACrashBetweenFileAndIndexIsSettled(t *testing.T) {
	dir := t.TempDir()
	store := reopen(t, dir)
	storeN(t, store, 0, 3)
	begin := func(n int, path string) {
		t.Helper()
		if _, err := store.appendJournal(journalRecord{Op: recordBegin, UID: uidOf(n), Path: path}, true); err != nil {
			t.Fatal(err)
		}
	}
	pathOf := func(n int) string {
		ds := generated(n)
		return fmt.Sprintf("%s/%s/%s.dcm", stringValue(ds, tagStudyInstanceUID), stringValue(ds, tagSeriesInstanceUID), uidOf(n))
	}

	// Instance 10: begun and written, and the store stopped before its put.
	begin(10, pathOf(10))
	full := filepath.Join(dir, filepath.FromSlash(pathOf(10)))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := writeInstance(full, "1.2.840.10008.5.1.4.1.1.2", uidOf(10), generated(10)); err != nil {
		t.Fatal(err)
	}
	// Instance 11: begun, and stopped before its file was in place.
	begin(11, pathOf(11))
	// Instance 1: a removal begun, its file deleted, and stopped there.
	begin(1, pathOf(1))
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(pathOf(1)))); err != nil {
		t.Fatal(err)
	}
	// Instance 2: a removal begun and stopped before its file went.
	begin(2, pathOf(2))

	got := reopen(t, dir)
	for n, want := range map[int]bool{0: true, 1: false, 2: true, 10: true, 11: false} {
		inst, ok := got.Instance(uidOf(n))
		if ok != want {
			t.Errorf("instance %d: held %v, want %v", n, ok, want)
			continue
		}
		if ok && inst.value(tagPatientID) != fmt.Sprintf("P%d", n/1000) {
			t.Errorf("instance %d is indexed without its keys: %v", n, inst.Keys)
		}
	}
}

// TestARecordCutShortIsDropped checks a journal whose last line a crash cut
// off: the store opens with every whole record, and the next record does
// not run on from the cut one, which would make the line after it unread.
func TestARecordCutShortIsDropped(t *testing.T) {
	dir := t.TempDir()
	storeN(t, reopen(t, dir), 0, 5)
	file, err := os.OpenFile(filepath.Join(dir, journalFileName), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"seq":99,"op":"put","instance":{"sopInstanceUID":"1.2`); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	store := reopen(t, dir)
	if store.Count() != 5 {
		t.Fatalf("with a record cut short the store holds %d, want 5", store.Count())
	}
	storeN(t, store, 5, 8)
	// Read here rather than by reopening, which would rebuild from the
	// files if a line did not read and so hide it.
	journal, _ := os.ReadFile(filepath.Join(dir, journalFileName))
	for i, line := range bytes.Split(bytes.TrimSuffix(journal, []byte("\n")), []byte("\n")) {
		var record journalRecord
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("journal line %d does not read: %q", i+1, line)
		}
	}
	if got := reopen(t, dir); got.Count() != 8 {
		t.Errorf("after storing past the cut record the store holds %d, want 8", got.Count())
	}
}

// TestAnIndexOfVersion3OpensAsItIs checks an archive written before the
// journal: its index is read, not rebuilt, and written again as version 4
// before anything is journaled. An entry in that index whose file is not
// on disk shows it was read: a rebuild from the files would not have it.
func TestAnIndexOfVersion3OpensAsItIs(t *testing.T) {
	dir := t.TempDir()
	storeN(t, reopen(t, dir), 0, 3)
	store := reopen(t, dir)
	entries := map[string]*Instance{}
	for _, inst := range store.Instances() {
		entries[inst.SOPInstanceUID] = inst
	}
	entries["1.2.3.999"] = &Instance{SOPClassUID: "1.2.840.10008.5.1.4.1.1.2", SOPInstanceUID: "1.2.3.999",
		Path: "only/in/the/index.dcm", Keys: map[string]IndexedValue{}}
	data, err := json.MarshalIndent(indexFile{Version: 3, Instances: entries}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, indexFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, journalFileName)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	got := reopen(t, dir)
	if _, ok := got.Instance("1.2.3.999"); !ok || got.Count() != 4 {
		t.Fatalf("the version 3 index was not read as it is: %d instances", got.Count())
	}
	var written indexFile
	data, _ = os.ReadFile(filepath.Join(dir, indexFileName))
	if err := json.Unmarshal(data, &written); err != nil || written.Version != indexVersion || len(written.Instances) != 4 {
		t.Errorf("after opening, the index is version %d with %d instances, want %d with 4",
			written.Version, len(written.Instances), indexVersion)
	}
}

// TestTheJournalIsFoldedIntoTheSnapshot checks that once the journal holds
// more records than the archive has instances it becomes the snapshot, so
// replaying it never costs more than reading the archive's index does.
func TestTheJournalIsFoldedIntoTheSnapshot(t *testing.T) {
	defer func(was int) { compactAfter = was }(compactAfter)
	compactAfter = 10
	dir := t.TempDir()
	store := reopen(t, dir)
	storeN(t, store, 0, 50)

	var snapshot indexFile
	data, _ := os.ReadFile(filepath.Join(dir, indexFileName))
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	journal, _ := os.ReadFile(filepath.Join(dir, journalFileName))
	records := bytes.Count(journal, []byte("\n"))
	// Folded once the records passed the instances, the journal is never
	// more than a record past them.
	if len(snapshot.Instances) == 0 || records > store.Count()+1 {
		t.Errorf("after 50 stores the snapshot holds %d and the journal %d records", len(snapshot.Instances), records)
	}
	if got := reopen(t, dir); !equal(held(got), held(store)) {
		t.Errorf("reopened, %d instances, want %d", got.Count(), store.Count())
	}
}

// TestConcurrentStoresAndRemovesSurviveAReopen stores, replaces and removes
// from eight goroutines at once, with the journal folded into the snapshot
// as they go, and checks the store reopens holding what it held.
func TestConcurrentStoresAndRemovesSurviveAReopen(t *testing.T) {
	defer func(was int) { compactAfter = was }(compactAfter)
	compactAfter = 64
	dir := t.TempDir()
	store := reopen(t, dir)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 60; i++ {
				// Neighbors share instances, so one UID is written by two
				// goroutines, and every third is removed again.
				n := g*50 + i
				if _, err := store.Store(context.Background(), generated(n)); err != nil {
					errs <- err
					return
				}
				if i%3 == 0 {
					if err := store.Remove(uidOf(n)); err != nil && !strings.Contains(err.Error(), "no instance") {
						errs <- err
						return
					}
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	want := held(store)
	if got := held(reopen(t, dir)); !equal(got, want) {
		t.Errorf("reopened, %d instances, want %d", len(got), len(want))
	}
	if got := held(reopen(t, dir)); !equal(got, want) {
		t.Errorf("reopened twice, %d instances, want %d", len(got), len(want))
	}
}

// TestAStoreKilledMidwayLosesNothingAcknowledged runs stores in a child
// process, kills it while it is storing, and opens what it left: every
// instance whose store had returned is held, and every instance file on
// disk is indexed, so neither a file nor an index entry outlives the other.
func TestAStoreKilledMidwayLosesNothingAcknowledged(t *testing.T) {
	if dir := os.Getenv("GODICOM_DCMSTORE_CHILD"); dir != "" {
		store, err := Open(dir)
		if err != nil {
			fmt.Println("open:", err)
			os.Exit(1)
		}
		for n := 0; ; n++ {
			if _, err := store.Store(context.Background(), generated(n)); err != nil {
				fmt.Println("store:", err)
				os.Exit(1)
			}
			fmt.Println(uidOf(n))
		}
	}
	if testing.Short() {
		t.Skip("kills a child process")
	}
	dir := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^TestAStoreKilledMidwayLosesNothingAcknowledged$")
	child.Env = append(os.Environ(), "GODICOM_DCMSTORE_CHILD="+dir)
	var out bytes.Buffer
	child.Stdout = &out
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()

	acknowledged := strings.Fields(out.String())
	if len(acknowledged) < 10 {
		t.Fatalf("the child stored %d instances before it was killed: %q", len(acknowledged), out.String())
	}
	store := reopen(t, dir)
	for _, uid := range acknowledged {
		if _, ok := store.Instance(uid); !ok {
			t.Errorf("%s was acknowledged and is not held after the kill", uid)
		}
	}
	files := 0
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".dcm") {
			return err
		}
		files++
		uid := strings.TrimSuffix(filepath.Base(path), ".dcm")
		if _, ok := store.Instance(uid); !ok {
			t.Errorf("%s is on disk and not indexed", uid)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files != store.Count() {
		t.Errorf("%d files on disk and %d indexed", files, store.Count())
	}
	t.Logf("killed after %d acknowledged stores; %d files, %d indexed", len(acknowledged), files, store.Count())
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
