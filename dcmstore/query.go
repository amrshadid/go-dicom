package dcmstore

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The attributes a query can match on, and the hierarchy it walks.
//
// PS3.4 C.6.1.1 and C.6.2.1 define the keys for each level of the Patient Root
// and Study Root information models. Only these are indexed: an archive that
// indexed every attribute of every instance would hold the whole data set in
// memory, which is what storing the files was meant to avoid.

// Tags used throughout. Declared here rather than inline so that a query key and
// the attribute it matches cannot drift apart.
var (
	tagQueryRetrieveLevel = tag.New(0x0008, 0x0052)

	tagSOPClassUID    = tag.New(0x0008, 0x0016)
	tagSOPInstanceUID = tag.New(0x0008, 0x0018)

	tagPatientName      = tag.New(0x0010, 0x0010)
	tagPatientID        = tag.New(0x0010, 0x0020)
	tagPatientBirthDate = tag.New(0x0010, 0x0030)
	tagPatientSex       = tag.New(0x0010, 0x0040)

	tagStudyDate              = tag.New(0x0008, 0x0020)
	tagStudyTime              = tag.New(0x0008, 0x0030)
	tagAccessionNumber        = tag.New(0x0008, 0x0050)
	tagModalitiesInStudy      = tag.New(0x0008, 0x0061)
	tagReferringPhysicianName = tag.New(0x0008, 0x0090)
	tagStudyDescription       = tag.New(0x0008, 0x1030)
	tagStudyInstanceUID       = tag.New(0x0020, 0x000D)
	tagStudyID                = tag.New(0x0020, 0x0010)

	tagModality          = tag.New(0x0008, 0x0060)
	tagSeriesDescription = tag.New(0x0008, 0x103E)
	tagSeriesInstanceUID = tag.New(0x0020, 0x000E)
	tagSeriesNumber      = tag.New(0x0020, 0x0011)

	tagInstanceNumber = tag.New(0x0020, 0x0013)

	tagNumberOfStudyRelatedSeries     = tag.New(0x0020, 0x1206)
	tagNumberOfStudyRelatedInstances  = tag.New(0x0020, 0x1208)
	tagNumberOfSeriesRelatedInstances = tag.New(0x0020, 0x1209)
)

// Level is a query/retrieve level, the value of QueryRetrieveLevel (0008,0052).
type Level string

const (
	LevelPatient Level = "PATIENT"
	LevelStudy   Level = "STUDY"
	LevelSeries  Level = "SERIES"
	LevelImage   Level = "IMAGE"
)

// levelOrder is the hierarchy, outermost first. A query at one level returns the
// keys of every level above it as well, since those identify what was found.
var levelOrder = []Level{LevelPatient, LevelStudy, LevelSeries, LevelImage}

// uniqueKey is the attribute that identifies one entity at a level. Grouping
// matched instances by it is what turns a set of instances into a set of
// patients, studies or series.
var uniqueKey = map[Level]tag.Tag{
	LevelPatient: tagPatientID,
	LevelStudy:   tagStudyInstanceUID,
	LevelSeries:  tagSeriesInstanceUID,
	LevelImage:   tagSOPInstanceUID,
}

// indexedTags is every attribute the index holds, with the VR to store it under
// when the element does not state one.
var indexedTags = func() map[tag.Tag]dataelem.VR {
	vrs := map[tag.Tag]dataelem.VR{
		tagPatientName:            dataelem.PN,
		tagPatientID:              dataelem.LO,
		tagPatientBirthDate:       dataelem.DA,
		tagPatientSex:             dataelem.CS,
		tagStudyDate:              dataelem.DA,
		tagStudyTime:              dataelem.TM,
		tagAccessionNumber:        dataelem.SH,
		tagModalitiesInStudy:      dataelem.CS,
		tagReferringPhysicianName: dataelem.PN,
		tagStudyDescription:       dataelem.LO,
		tagStudyInstanceUID:       dataelem.UI,
		tagStudyID:                dataelem.SH,
		tagModality:               dataelem.CS,
		tagSeriesDescription:      dataelem.LO,
		tagSeriesInstanceUID:      dataelem.UI,
		tagSeriesNumber:           dataelem.IS,
		tagSOPClassUID:            dataelem.UI,
		tagSOPInstanceUID:         dataelem.UI,
		tagInstanceNumber:         dataelem.IS,
	}
	return vrs
}()

// computedKeys are returned from the index rather than read from an instance:
// they count what the store holds, so no single instance carries them.
var computedKeys = map[tag.Tag]bool{
	tagNumberOfStudyRelatedSeries:     true,
	tagNumberOfStudyRelatedInstances:  true,
	tagNumberOfSeriesRelatedInstances: true,
	tagModalitiesInStudy:              true,
}

// indexKeys pulls the query attributes out of a data set.
//
// The VR is taken from the element where it has one, and from indexedTags
// otherwise. That order matters: an implicit VR file carries no VR at all, and a
// non-conformant one may carry the wrong VR — matching by what the element says
// keeps a query against this store consistent with a query against the file.
func indexKeys(ds *dataset.Dataset) map[string]IndexedValue {
	keys := make(map[string]IndexedValue, len(indexedTags))

	for t, fallback := range indexedTags {
		elem, ok := ds.Get(t)
		if !ok {
			continue
		}

		vr := elem.GetVR()
		if vr == "" || !dataelem.IsValidVR(vr) {
			vr = fallback
		}

		keys[t.String()] = IndexedValue{VR: string(vr), Value: elementString(elem)}
	}

	return keys
}

// QueryLevel reads the level a query asks for.
func QueryLevel(query *dataset.Dataset) (Level, error) {
	value := strings.ToUpper(strings.TrimSpace(stringValue(query, tagQueryRetrieveLevel)))
	if value == "" {
		return "", fmt.Errorf("the query has no QueryRetrieveLevel (0008,0052), " +
			"so there is no way to tell what it is asking for")
	}

	level := Level(value)
	for _, known := range levelOrder {
		if level == known {
			return level, nil
		}
	}
	return "", fmt.Errorf("QueryRetrieveLevel %q is not one of PATIENT, STUDY, SERIES or IMAGE", value)
}

// Query answers a C-FIND, returning one data set per matching entity at the
// level the query names.
//
// Matching follows PS3.4 C.2.2: every attribute the query carries a value for
// must match, and an attribute with a zero-length value matches anything and is
// returned. Attributes this store does not index are treated as unsupported
// optional keys — returned with a zero-length value, as C.2.2.1.2 allows — rather
// than matched, since matching on an attribute that was never indexed would
// silently return nothing.
func (s *Store) Query(ctx context.Context, query *dataset.Dataset) ([]*dataset.Dataset, error) {
	level, err := QueryLevel(query)
	if err != nil {
		return nil, err
	}

	matched, err := s.matchInstances(ctx, query, level)
	if err != nil {
		return nil, err
	}

	return s.buildResponses(query, level, matched), nil
}

// MatchingInstances answers a C-GET or C-MOVE: the instances to transfer, rather
// than one response per entity.
//
// A retrieval at STUDY level transfers every instance in the matching studies, so
// this returns instances whatever level the query names.
func (s *Store) MatchingInstances(ctx context.Context, query *dataset.Dataset) ([]*Instance, error) {
	level, err := QueryLevel(query)
	if err != nil {
		return nil, err
	}
	return s.matchInstances(ctx, query, level)
}

// matchInstances returns the instances satisfying a query.
func (s *Store) matchInstances(ctx context.Context, query *dataset.Dataset, level Level) ([]*Instance, error) {
	if query == nil {
		return nil, fmt.Errorf("the query is nil")
	}

	// The keys to match on: every attribute in the query except the level itself,
	// the computed counts, and anything not indexed.
	type criterion struct {
		vr    dataelem.VR
		tag   tag.Tag
		value string
	}
	var criteria []criterion

	for _, elem := range query.GetAll() {
		t, ok := elem.Tag()
		if !ok || t == tagQueryRetrieveLevel || computedKeys[t] {
			continue
		}

		value := elementString(elem)
		if trimPadding(value) == "" {
			// Universal matching: it selects nothing and is returned in the
			// response, which buildResponses handles.
			continue
		}

		vr, indexed := indexedTags[t]
		if !indexed {
			// An unsupported optional key. Matching on it would return nothing at
			// all, which is a worse answer than ignoring it — C.2.2.1.2 permits an
			// SCP to ignore optional keys it does not support.
			storeLogger.Debug("dcmstore: ignoring %s in a query; it is not indexed", t.String())
			continue
		}
		if elemVR := elem.GetVR(); elemVR != "" && dataelem.IsValidVR(elemVR) {
			vr = elemVR
		}

		criteria = append(criteria, criterion{vr: vr, tag: t, value: value})
	}

	s.mu.RLock()
	candidates := make([]*Instance, 0, len(s.instances))
	for _, inst := range s.instances {
		candidates = append(candidates, inst)
	}
	s.mu.RUnlock()

	var matched []*Instance
	for _, inst := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		keep := true
		for _, c := range criteria {
			if !matchValue(c.vr, c.value, inst.value(c.tag)) {
				keep = false
				break
			}
		}
		if keep {
			matched = append(matched, inst)
		}
	}

	// A stable order, so that repeating a query gives the same answer in the same
	// sequence — which a requestor paging through results depends on.
	sort.Slice(matched, func(i, j int) bool {
		return instanceOrder(matched[i]) < instanceOrder(matched[j])
	})

	_ = level
	return matched, nil
}

// instanceOrder is the sort key giving results a stable, hierarchical order.
func instanceOrder(inst *Instance) string {
	return strings.Join([]string{
		inst.value(tagPatientID),
		inst.value(tagStudyInstanceUID),
		inst.value(tagSeriesInstanceUID),
		padNumeric(inst.value(tagInstanceNumber)),
		inst.SOPInstanceUID,
	}, "\x00")
}

// padNumeric left-pads an integer string so that it sorts numerically.
//
// InstanceNumber is IS, so "10" sorts before "9" as a string. Frames come back
// in the wrong order without this, which looks like a data problem rather than a
// sorting one.
func padNumeric(value string) string {
	value = trimPadding(value)
	if value == "" {
		return value
	}
	if _, err := strconv.Atoi(value); err != nil {
		return value
	}
	if len(value) >= 12 {
		return value
	}
	return strings.Repeat("0", 12-len(value)) + value
}

// buildResponses groups matched instances by the level's unique key and builds one
// response data set for each.
func (s *Store) buildResponses(query *dataset.Dataset, level Level, matched []*Instance) []*dataset.Dataset {
	unique := uniqueKey[level]

	// Group, preserving the order matched arrived in so responses stay stable.
	var order []string
	groups := make(map[string][]*Instance)
	for _, inst := range matched {
		id := inst.value(unique)
		if _, seen := groups[id]; !seen {
			order = append(order, id)
		}
		groups[id] = append(groups[id], inst)
	}

	responses := make([]*dataset.Dataset, 0, len(order))
	for _, id := range order {
		responses = append(responses, s.buildResponse(query, level, groups[id]))
	}
	return responses
}

// buildResponse builds one C-FIND response.
//
// It carries QueryRetrieveLevel, the unique key of every level down to this one —
// which is what lets the requestor use the result as the query for the next level
// down — and every attribute the query asked for.
func (s *Store) buildResponse(query *dataset.Dataset, level Level, group []*Instance) *dataset.Dataset {
	response := dataset.NewDataset()
	representative := group[0]

	_ = response.Add(dataelem.NewDataElement(tagQueryRetrieveLevel, dataelem.CS, string(level)))

	// The unique keys down the hierarchy. Type 1 in a response: without them the
	// requestor cannot address what was returned.
	for _, l := range levelOrder {
		key := uniqueKey[l]
		if value := representative.value(key); value != "" {
			vr := indexedTags[key]
			_ = response.Add(dataelem.NewDataElement(key, vr, value))
		}
		if l == level {
			break
		}
	}

	// Then everything the query asked for.
	for _, elem := range query.GetAll() {
		t, ok := elem.Tag()
		if !ok || t == tagQueryRetrieveLevel || response.Contains(t) {
			continue
		}

		if computedKeys[t] {
			if value := s.computed(t, group); value != "" {
				_ = response.Add(dataelem.NewDataElement(t, computedVR(t), value))
			}
			continue
		}

		vr, indexed := indexedTags[t]
		if !indexed {
			// An unsupported optional key is returned with a zero-length value, so
			// the requestor can see it was understood as a key and not matched on.
			vr = elem.GetVR()
			if vr == "" || !dataelem.IsValidVR(vr) {
				vr = dataelem.UN
			}
			_ = response.Add(dataelem.NewDataElement(t, vr, ""))
			continue
		}

		_ = response.Add(dataelem.NewDataElement(t, vr, representative.value(t)))
	}

	return response
}

// computed returns the value of an attribute derived from the index.
func (s *Store) computed(t tag.Tag, group []*Instance) string {
	switch t {
	case tagNumberOfSeriesRelatedInstances:
		// Every instance in the group is in one series only when the query was at
		// SERIES level or below; count the group's own series to be safe.
		return strconv.Itoa(countInstancesInSeries(s, group))

	case tagNumberOfStudyRelatedInstances:
		return strconv.Itoa(countInstancesInStudy(s, group))

	case tagNumberOfStudyRelatedSeries:
		return strconv.Itoa(countSeriesInStudy(s, group))

	case tagModalitiesInStudy:
		return strings.Join(modalitiesInStudy(s, group), `\`)

	default:
		return ""
	}
}

// computedVR is the VR each computed attribute is returned with.
func computedVR(t tag.Tag) dataelem.VR {
	if t == tagModalitiesInStudy {
		return dataelem.CS
	}
	return dataelem.IS
}

// The counts are over the whole store, not the matched set: PS3.4 C.6.2.2 defines
// NumberOfStudyRelatedInstances as the number of instances in the study, and
// answering with the number that matched the query would understate it whenever
// the query narrowed by anything.

func countInstancesInStudy(s *Store, group []*Instance) int {
	studyUID := group[0].value(tagStudyInstanceUID)
	count := 0
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, inst := range s.instances {
		if inst.value(tagStudyInstanceUID) == studyUID {
			count++
		}
	}
	return count
}

func countSeriesInStudy(s *Store, group []*Instance) int {
	studyUID := group[0].value(tagStudyInstanceUID)
	series := make(map[string]bool)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, inst := range s.instances {
		if inst.value(tagStudyInstanceUID) == studyUID {
			series[inst.value(tagSeriesInstanceUID)] = true
		}
	}
	return len(series)
}

func countInstancesInSeries(s *Store, group []*Instance) int {
	seriesUID := group[0].value(tagSeriesInstanceUID)
	count := 0
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, inst := range s.instances {
		if inst.value(tagSeriesInstanceUID) == seriesUID {
			count++
		}
	}
	return count
}

func modalitiesInStudy(s *Store, group []*Instance) []string {
	studyUID := group[0].value(tagStudyInstanceUID)
	seen := make(map[string]bool)
	s.mu.RLock()
	for _, inst := range s.instances {
		if inst.value(tagStudyInstanceUID) != studyUID {
			continue
		}
		if modality := inst.value(tagModality); modality != "" {
			seen[modality] = true
		}
	}
	s.mu.RUnlock()

	out := make([]string, 0, len(seen))
	for modality := range seen {
		out = append(out, modality)
	}
	sort.Strings(out)
	return out
}
