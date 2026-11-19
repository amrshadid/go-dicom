package matching

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

var (
	tagPatientName = tag.New(0x0010, 0x0010)
	tagPatientID   = tag.New(0x0010, 0x0020)
	tagStudyDate   = tag.New(0x0008, 0x0020)
	tagModality    = tag.New(0x0008, 0x0060)
	tagImageType   = tag.New(0x0008, 0x0008)
	tagStudyUID    = tag.New(0x0020, 0x000D)
	tagSPSComment  = tag.New(0x0040, 0x0400) // LT
	tagCharset     = tag.New(0x0008, 0x0005)
	tagQRLevel     = tag.New(0x0008, 0x0052)
	tagSPS         = tag.New(0x0040, 0x0100)
	tagStationAET  = tag.New(0x0040, 0x0001)
	tagSPSDate     = tag.New(0x0040, 0x0002)
	tagSPSID       = tag.New(0x0040, 0x0009)
	tagPreMedLO    = tag.New(0x0040, 0x0012)
)

// attr is one element of a data set built for a test. Values are bytes, as a
// data set read from the network or a file holds them.
type attr struct {
	tag   tag.Tag
	vr    dataelem.VR
	value string
}

func build(t *testing.T, attrs ...attr) *dataset.Dataset {
	t.Helper()
	ds := dataset.NewDataset()
	for _, a := range attrs {
		if err := ds.Add(dataelem.NewDataElement(a.tag, a.vr, []byte(a.value))); err != nil {
			t.Fatal(err)
		}
	}
	return ds
}

// withSequence adds a sequence of the given items to ds.
func withSequence(t *testing.T, ds *dataset.Dataset, seqTag tag.Tag, items ...*dataset.Dataset) *dataset.Dataset {
	t.Helper()
	seq := sequence.New()
	for _, item := range items {
		if err := seq.Append(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := ds.AddSequence(seqTag, seq); err != nil {
		t.Fatal(err)
	}
	return ds
}

func mustMatch(t *testing.T, query, candidate *dataset.Dataset) bool {
	t.Helper()
	id, err := Compile(query, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return id.MatchDataset(candidate)
}

// TestKeysMatchByTheRulesOfC2222 runs each value rule of PS3.4 C.2.2.2 over
// one key against one candidate: universal, single value with the case rule
// for person names, list of UID, wildcard by character, range with partial
// and ACR-NEMA dates, empty value and keys of more than one value.
func TestKeysMatchByTheRulesOfC2222(t *testing.T) {
	smith := build(t,
		attr{tagPatientName, dataelem.PN, "SMITH^JOHN "},
		attr{tagPatientID, dataelem.LO, "PID1"},
		attr{tagStudyDate, dataelem.DA, "1997.04.24"},
		attr{tagModality, dataelem.CS, "MR"},
		attr{tagImageType, dataelem.CS, `ORIGINAL\PRIMARY\AXIAL`},
		attr{tagStudyUID, dataelem.UI, "1.2.3.4"},
		attr{tagSPSComment, dataelem.LT, `a\b`},
	)
	noDate := build(t, attr{tagCharset, dataelem.CS, "ISO_IR 192"}, attr{tagPatientName, dataelem.PN, "Müller^Hans"}, attr{tagModality, dataelem.CS, ""})

	cases := []struct {
		name      string
		key       attr
		candidate *dataset.Dataset
		want      bool
	}{
		{"universal", attr{tagPatientName, dataelem.PN, ""}, smith, true},
		{"universal on an absent attribute", attr{tagStudyDate, dataelem.DA, ""}, build(t), true},
		{"single value, padding ignored", attr{tagPatientName, dataelem.PN, "SMITH^JOHN"}, smith, true},
		{"single value, another name", attr{tagPatientName, dataelem.PN, "SMITH^JANE"}, smith, false},
		{"person name whatever its case", attr{tagPatientName, dataelem.PN, "smith^john"}, smith, true},
		{"code string as written", attr{tagModality, dataelem.CS, "mr"}, smith, false},
		{"accents kept", attr{tagPatientName, dataelem.PN, "Muller^Hans"}, noDate, false},
		{"list of UID", attr{tagStudyUID, dataelem.UI, `1.2.3.9\1.2.3.4`}, smith, true},
		{"list of UID, none", attr{tagStudyUID, dataelem.UI, `1.2.3.9\1.2.3.5`}, smith, false},
		{"a star in a UID is a star", attr{tagStudyUID, dataelem.UI, "1.2.*"}, smith, false},
		{"wildcard star", attr{tagPatientName, dataelem.PN, "SMI*"}, smith, true},
		{"wildcard star and case", attr{tagPatientName, dataelem.PN, "*john"}, smith, true},
		{"question mark is one character", attr{tagPatientName, dataelem.PN, "M?ller*"}, noDate, true},
		{"star matches an absent attribute", attr{tagPatientID, dataelem.LO, "*"}, noDate, true},
		{"a value of a multi-valued candidate", attr{tagImageType, dataelem.CS, "AXIAL"}, smith, true},
		{"range over an ACR-NEMA date", attr{tagStudyDate, dataelem.DA, "19970101-19971231"}, smith, true},
		{"range of a partial year", attr{tagStudyDate, dataelem.DA, "1997-1997"}, smith, true},
		{"open upper bound", attr{tagStudyDate, dataelem.DA, "19980101-"}, smith, false},
		{"open lower bound", attr{tagStudyDate, dataelem.DA, "-19970424"}, smith, true},
		{"a range does not match no date", attr{tagStudyDate, dataelem.DA, "-20991231"}, noDate, false},
		{"empty value, empty attribute", attr{tagModality, dataelem.CS, `""`}, noDate, true},
		{"empty value, absent attribute", attr{tagStudyDate, dataelem.DA, `""`}, noDate, true},
		{"empty value, attribute with a value", attr{tagModality, dataelem.CS, `""`}, smith, false},
		{"multiple values, one matches", attr{tagModality, dataelem.CS, `CT\MR`}, smith, true},
		{"multiple values, none matches", attr{tagModality, dataelem.CS, `CT\US`}, smith, false},
		{"multiple values with wildcards", attr{tagPatientName, dataelem.PN, `JONES*\SMITH*`}, smith, true},
		{"an empty value among several is not universal", attr{tagModality, dataelem.CS, `CT\`}, smith, false},
		{"a backslash in LT is text", attr{tagSPSComment, dataelem.LT, `a\b`}, smith, true},
		{"a backslash in LT is not a separator", attr{tagSPSComment, dataelem.LT, `a\z`}, smith, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mustMatch(t, build(t, tc.key), tc.candidate)
			if got != tc.want {
				t.Errorf("key %s %q against the candidate: match = %v, want %v",
					tc.key.tag, tc.key.value, got, tc.want)
			}
		})
	}
}

// TestEveryKeyMustMatch: a candidate matches only when all of the keys do.
func TestEveryKeyMustMatch(t *testing.T) {
	candidate := build(t, attr{tagPatientName, dataelem.PN, "SMITH^JOHN"}, attr{tagModality, dataelem.CS, "MR"})
	query := build(t, attr{tagPatientName, dataelem.PN, "SMITH*"}, attr{tagModality, dataelem.CS, "CT"})
	if mustMatch(t, query, candidate) {
		t.Error("a candidate matching the name and not the modality matched")
	}
}

// step builds one Scheduled Procedure Step item.
func step(t *testing.T, station, date, id string) *dataset.Dataset {
	return build(t,
		attr{tagStationAET, dataelem.AE, station},
		attr{tagSPSDate, dataelem.DA, date},
		attr{tagSPSID, dataelem.SH, id},
		attr{tagPreMedLO, dataelem.LO, "NONE"},
	)
}

// TestSequenceKeysAreMetByOneItem: a worklist entry with a step at station A
// on Monday and another at B on Tuesday matches A on Monday and B on Tuesday,
// and not A on Tuesday (PS3.4 C.2.2.2.6, K.6.1.2.2): the keys of the query
// item must all be met by the same stored item.
func TestSequenceKeysAreMetByOneItem(t *testing.T) {
	entry := withSequence(t, build(t, attr{tagPatientName, dataelem.PN, "DOE^JANE"}), tagSPS,
		step(t, "STA", "20261116", "S1"), step(t, `STB\STC`, "20261117", "S2"))

	cases := []struct {
		station, date string
		want          bool
	}{
		{"STA", "20261116", true},
		{"STB", "20261117", true},
		{"STC", "", true}, // one value of a multi-valued station
		{"STA", "20261117", false},
		{"ST*", "20261117", true},
		{"", "20261115-20261116", true},
		{"STZ", "", false},
	}
	for _, tc := range cases {
		query := withSequence(t, build(t, attr{tagPatientName, dataelem.PN, ""}), tagSPS,
			build(t, attr{tagStationAET, dataelem.AE, tc.station}, attr{tagSPSDate, dataelem.DA, tc.date}))
		if got := mustMatch(t, query, entry); got != tc.want {
			t.Errorf("station %q date %q: match = %v, want %v", tc.station, tc.date, got, tc.want)
		}
	}
}

// TestUniversalSequenceKeys: a sequence key with no item, or with an item of
// universal keys only, selects nothing, and matches a candidate that does not
// have the sequence; one with a value does not.
func TestUniversalSequenceKeys(t *testing.T) {
	bare := build(t, attr{tagPatientName, dataelem.PN, "DOE^JANE"})
	noItem := withSequence(t, build(t), tagSPS)
	universalItem := withSequence(t, build(t), tagSPS, build(t, attr{tagStationAET, dataelem.AE, ""}))
	withValue := withSequence(t, build(t), tagSPS, build(t, attr{tagStationAET, dataelem.AE, "STA"}))

	if !mustMatch(t, noItem, bare) {
		t.Error("a sequence key with no item did not match a candidate without the sequence")
	}
	if !mustMatch(t, universalItem, bare) {
		t.Error("a sequence key of universal keys did not match a candidate without the sequence")
	}
	if mustMatch(t, withValue, bare) {
		t.Error("a sequence key with a value matched a candidate without the sequence")
	}
}

// TestSkippedKeysAreNotMatched: a key Options.Skip names is passed over, at
// the top of the query and inside a sequence item, so a caller that cannot
// match on an attribute does not answer with nothing.
func TestSkippedKeysAreNotMatched(t *testing.T) {
	entry := withSequence(t, build(t, attr{tagPatientName, dataelem.PN, "DOE^JANE"}), tagSPS,
		step(t, "STA", "20261116", "S1"))
	query := withSequence(t, build(t, attr{tagQRLevel, dataelem.CS, "WORKLIST"}), tagSPS,
		build(t, attr{tagPreMedLO, dataelem.LO, "SOMETHING"}))

	if mustMatch(t, query, entry) {
		t.Fatal("the query matched with nothing skipped; the test proves nothing")
	}
	skip := func(parent tag.Tag, elem *dataelem.DataElement) bool {
		key, _ := elem.Tag()
		return (parent == 0 && key == tagQRLevel) || (parent == tagSPS && key == tagPreMedLO)
	}
	id, err := Compile(query, Options{Skip: skip})
	if err != nil {
		t.Fatal(err)
	}
	if !id.MatchDataset(entry) {
		t.Error("the query did not match with its two unsupported keys skipped")
	}
}

// TestCompileRefusesNoQuery: there is no identifier to read from nil.
func TestCompileRefusesNoQuery(t *testing.T) {
	if _, err := Compile(nil, Options{}); err == nil {
		t.Error("Compile(nil) returned no error")
	}
	if _, err := Response(nil, dataset.NewDataset(), Options{}); err == nil {
		t.Error("Response with no query returned no error")
	}
}

// corpusFile reads a file of pydicom's charset_files, next to the test_files
// directory GODICOM_PYDICOM_DATA names.
func corpusFile(t *testing.T, name string) *dataset.Dataset {
	t.Helper()
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	file, err := os.Open(filepath.Join(dir, "..", "charset_files", name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(file))
	if err != nil {
		t.Fatal(err)
	}
	return df.GetDataset()
}

// TestQueryAndCandidateAreEachReadInTheirOwnCharacterSet: pydicom's
// chrFren.dcm is in ISO_IR 100 and chrH31.dcm in ISO 2022 IR 87; a query in
// UTF-8 or in Latin-1 finds each by its name with accents or kanji, and a
// query written in one set does not match a name only because the bytes
// agree.
func TestQueryAndCandidateAreEachReadInTheirOwnCharacterSet(t *testing.T) {
	french := corpusFile(t, "chrFren.dcm")  // Buc^Jérôme
	japanese := corpusFile(t, "chrH31.dcm") // Yamada^Tarou=山田^太郎=やまだ^たろう

	cases := []struct {
		name      string
		charset   string
		value     string
		candidate *dataset.Dataset
		want      bool
	}{
		{"UTF-8 query, Latin-1 file", "ISO_IR 192", "Buc^Jérôme", french, true},
		{"UTF-8 wildcard by character", "ISO_IR 192", "Buc^J?r?me", french, true},
		{"Latin-1 query, Latin-1 file", "ISO_IR 100", "Buc^J\xe9r\xf4me", french, true},
		{"Latin-1 bytes read as UTF-8 are not the name", "ISO_IR 192", "Buc^J\xe9r\xf4me", french, false},
		{"UTF-8 query, ISO 2022 file", "ISO_IR 192", "*=山田^太郎=*", japanese, true},
		{"accents are kept", "ISO_IR 192", "Buc^Jerome", french, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query := build(t, attr{tagCharset, dataelem.CS, tc.charset}, attr{tagPatientName, dataelem.PN, tc.value})
			if got := mustMatch(t, query, tc.candidate); got != tc.want {
				t.Errorf("%s %q: match = %v, want %v", tc.charset, tc.value, got, tc.want)
			}
		})
	}
}
