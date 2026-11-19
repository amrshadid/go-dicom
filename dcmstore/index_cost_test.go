package dcmstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// generated is instance n of a generated archive: 100 instances a series,
// 10 series a study.
func generated(n int) *dataset.Dataset {
	ds := dataset.NewDataset()
	study, series := n/1000, n/100
	for _, e := range []struct {
		t     tag.Tag
		vr    dataelem.VR
		value string
	}{
		{tag.New(0x0008, 0x0016), dataelem.UI, "1.2.840.10008.5.1.4.1.1.2"},
		{tag.New(0x0008, 0x0018), dataelem.UI, fmt.Sprintf("1.2.826.0.1.3680043.10.9.%d.%d.%d", study, series, n)},
		{tag.New(0x0010, 0x0010), dataelem.PN, fmt.Sprintf("PATIENT^%d", study)},
		{tag.New(0x0010, 0x0020), dataelem.LO, fmt.Sprintf("P%d", study)},
		{tag.New(0x0020, 0x000D), dataelem.UI, fmt.Sprintf("1.2.826.0.1.3680043.10.9.%d", study)},
		{tag.New(0x0008, 0x0020), dataelem.DA, "20240115"},
		{tag.New(0x0020, 0x000E), dataelem.UI, fmt.Sprintf("1.2.826.0.1.3680043.10.9.%d.%d", study, series)},
		{tag.New(0x0008, 0x0060), dataelem.CS, "CT"},
		{tag.New(0x0020, 0x0013), dataelem.IS, fmt.Sprint(n % 100)},
	} {
		_ = ds.Add(dataelem.NewDataElement(e.t, e.vr, e.value))
	}
	return ds
}

// TestMeasureIndexCost times a thousand stores at the start of an archive
// and a thousand at 99,000 instances, which is how the cost of keeping the
// index was measured (#410). It proves nothing on its own, since timings
// depend on the disk, so it runs only when GODICOM_MEASURE_INDEX is set:
//
//	GODICOM_MEASURE_INDEX=1 go test ./dcmstore -run TestMeasureIndexCost -v
func TestMeasureIndexCost(t *testing.T) {
	if os.Getenv("GODICOM_MEASURE_INDEX") == "" {
		t.Skip("set GODICOM_MEASURE_INDEX to measure")
	}
	ctx := context.Background()
	timeThousand := func(store *Store, from int) time.Duration {
		start := time.Now()
		for n := from; n < from+1000; n++ {
			if _, err := store.Store(ctx, generated(n)); err != nil {
				t.Fatal(err)
			}
		}
		return time.Since(start)
	}

	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := timeThousand(store, 0)
	t.Logf("instances 1-1000: %v (%.2f ms each); index after: %d bytes", first, float64(first.Microseconds())/1000/1000, indexSize(dir))

	// The archive at 99,000: the index as Store would have left it, its
	// entries made as Store makes them, without writing 98,000 more files
	// first, which at the cost measured would take hours.
	sample := make(map[string]*Instance, 99000)
	for _, inst := range store.Instances() {
		sample[inst.SOPInstanceUID] = inst
	}
	for n := 1000; n < 99000; n++ {
		ds := generated(n)
		sample[stringValue(ds, tagSOPInstanceUID)] = &Instance{
			SOPClassUID: "1.2.840.10008.5.1.4.1.1.2", SOPInstanceUID: stringValue(ds, tagSOPInstanceUID),
			Path: fmt.Sprintf("x/%d.dcm", n), Keys: indexKeys(ds), Sequences: indexSequences(ds),
		}
	}
	data, err := json.MarshalIndent(indexFile{Version: indexVersion, Instances: sample}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, indexFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	store, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Open at %d instances: %v", store.Count(), time.Since(start))
	last := timeThousand(store, 99000)
	t.Logf("instances 99001-100000: %v (%.2f ms each); index after: %d bytes", last, float64(last.Microseconds())/1000/1000, indexSize(dir))
	t.Logf("last thousand / first thousand: %.1fx", float64(last)/float64(first))
}

// indexSize is the size of the index file in dir, or -1.
func indexSize(dir string) int64 {
	info, err := os.Stat(filepath.Join(dir, indexFileName))
	if err != nil {
		return -1
	}
	return info.Size()
}
