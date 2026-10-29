package dcmstore_test

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/tag"
)

// dcmFiles lists the instance files under a store, relative to its root.
func dcmFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".dcm") {
			rel, _ := filepath.Rel(root, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func fileOf(t *testing.T, store *dcmstore.Store, uid string) []byte {
	t.Helper()
	inst, ok := store.Instance(uid)
	if !ok {
		t.Fatalf("%s is not in the index", uid)
	}
	data, err := os.ReadFile(filepath.Join(store.Root(), filepath.FromSlash(inst.Path)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestConcurrentStoresOfOneInstanceLeaveOneWholeCopy: two C-STOREs of the same
// SOP Instance UID at once, one small and one large. Both opened the same path
// with os.Create and wrote into it, so the file left behind was usually the
// small one's bytes followed by the rest of the large one's: neither copy,
// while the index described one of them (#189).
func TestConcurrentStoresOfOneInstanceLeaveOneWholeCopy(t *testing.T) {
	ctx := context.Background()
	version := func(name string, pixels int) *dataset.Dataset {
		ds := instance{patientID: "P1", patientName: name, studyUID: "1.2.1", seriesUID: "1.2.1.1",
			sopInstanceUID: "1.2.1.1.1", modality: "CT"}.dataset()
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, make([]byte, pixels)))
		return ds
	}
	short, long := version("SHORT^ONE", 64), version("LONG^ONE", 256<<10)

	alone := func(ds *dataset.Dataset) []byte {
		s, err := dcmstore.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Store(ctx, ds); err != nil {
			t.Fatal(err)
		}
		return fileOf(t, s, "1.2.1.1.1")
	}
	shortBytes, longBytes := alone(short), alone(long)
	nameTag := tag.New(0x0010, 0x0010).String()

	for trial := 0; trial < 40; trial++ {
		store, err := dcmstore.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for _, ds := range []*dataset.Dataset{short, long} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := store.Store(ctx, ds); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()

		data := fileOf(t, store, "1.2.1.1.1")
		isShort, isLong := bytes.Equal(data, shortBytes), bytes.Equal(data, longBytes)
		if !isShort && !isLong {
			t.Fatalf("trial %d: the file is %d bytes, neither the %d-byte nor the %d-byte copy",
				trial, len(data), len(shortBytes), len(longBytes))
		}
		inst, _ := store.Instance("1.2.1.1.1")
		if indexedLong := strings.HasPrefix(inst.Keys[nameTag].Value, "LONG"); indexedLong != isLong {
			t.Fatalf("trial %d: the file is the long copy: %v; the index describes it: %v",
				trial, isLong, indexedLong)
		}
	}
}

// TestReStoringUnderAnotherSeriesMovesTheFile: an instance stored again under
// a different series is written to a different path. The old file stayed, so
// Remove left a copy on disk and Rebuild brought the removed instance back
// (#189).
func TestReStoringUnderAnotherSeriesMovesTheFile(t *testing.T) {
	ctx := context.Background()
	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first := instance{patientID: "P1", studyUID: "1.2.9", seriesUID: "1.2.9.1",
		sopInstanceUID: "1.2.3.4", modality: "CT"}
	moved := first
	moved.studyUID, moved.seriesUID, moved.modality = "1.2.1", "1.2.1.1", "MR"
	for _, i := range []instance{first, moved} {
		if _, err := store.Store(ctx, i.dataset()); err != nil {
			t.Fatal(err)
		}
	}
	if files := dcmFiles(t, store.Root()); len(files) != 1 {
		t.Fatalf("after re-storing under another series, the store holds %v", files)
	}

	if err := store.Remove("1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	if err := store.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	if got := store.Count(); got != 0 {
		t.Fatalf("Rebuild brought back %d removed instance(s): %v", got, dcmFiles(t, store.Root()))
	}
}

// TestRebuildKeepsWhatIsStoredMeanwhile: Rebuild walked the tree and then
// replaced the index with what it had found, so an instance stored after the
// walk had passed its directory was dropped from the index although its file
// was on disk and the C-STORE had succeeded (#189).
func TestRebuildKeepsWhatIsStoredMeanwhile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := dcmstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 300; i++ {
		ds := instance{patientID: "P1", studyUID: "1.2.1", seriesUID: fmt.Sprintf("1.2.1.%d", i),
			sopInstanceUID: fmt.Sprintf("1.2.1.%d.1", i), modality: "CT"}.dataset()
		if _, err := store.Store(ctx, ds); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := store.Rebuild(ctx); err != nil {
			t.Error(err)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			ds := instance{patientID: "P2", studyUID: "1.2.2", seriesUID: fmt.Sprintf("1.2.2.%d", i),
				sopInstanceUID: fmt.Sprintf("1.2.2.%d.1", i), modality: "MR"}.dataset()
			if _, err := store.Store(ctx, ds); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()

	if got := store.Count(); got != 350 {
		t.Errorf("the index holds %d instances after a rebuild during 50 stores, want 350", got)
	}
	reopened, err := dcmstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Count(); got != 350 {
		t.Errorf("reopened, the index holds %d instances, want 350", got)
	}
}

// TestConcurrentStoresAllSurviveAReopen: each Store saves the index. The
// snapshot was taken under the lock and the file renamed outside it, so an
// older snapshot could be renamed in last, and an instance acknowledged with
// Success was missing from the index after a restart (#189). The window is
// narrow; this repeats enough to land in it.
func TestConcurrentStoresAllSurviveAReopen(t *testing.T) {
	ctx := context.Background()
	for trial := 0; trial < 20; trial++ {
		dir := t.TempDir()
		store, err := dcmstore.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ds := instance{patientID: "P1", studyUID: "1.2.1", seriesUID: "1.2.1.1",
					sopInstanceUID: fmt.Sprintf("1.2.1.1.%d", i), modality: "CT"}.dataset()
				if _, err := store.Store(ctx, ds); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		reopened, err := dcmstore.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := reopened.Count(); got != 100 {
			t.Fatalf("trial %d: reopened, the index holds %d of the 100 instances stored", trial, got)
		}
	}
}
