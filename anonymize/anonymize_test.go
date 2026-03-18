package anonymize_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

func TestNewAnonymizer(t *testing.T) {
	anon := anonymize.NewAnonymizer(anonymize.BasicProfile)
	if anon == nil {
		t.Fatal("NewAnonymizer returned nil")
	}
}

func TestAnonymizeNilDataset(t *testing.T) {
	anon := anonymize.NewAnonymizer(anonymize.BasicProfile)
	err := anon.Anonymize(nil)
	if err == nil {
		t.Error("Expected error for nil dataset")
	}
}

func TestAnonymizeEmptyDataset(t *testing.T) {
	anon := anonymize.NewAnonymizer(anonymize.BasicProfile)
	ds := dataset.NewDataset()
	err := anon.Anonymize(ds)
	if err != nil {
		t.Errorf("Unexpected error for empty dataset: %v", err)
	}
}

func TestSetRetainPatientName(t *testing.T) {
	anon := anonymize.NewAnonymizer(anonymize.BasicProfile)
	anon.SetRetainPatientName(true)
	// Should not panic
}

func TestSetRetainPatientID(t *testing.T) {
	anon := anonymize.NewAnonymizer(anonymize.BasicProfile)
	anon.SetRetainPatientID(true)
	// Should not panic
}

func TestSetCustomAction(t *testing.T) {
	anon := anonymize.NewAnonymizer(anonymize.BasicProfile)
	patientName := tag.New(0x0010, 0x0010)
	anon.SetCustomAction(patientName, anonymize.ActionKeep)
	// Should not panic
}

func TestUIDMapping(t *testing.T) {
	anon := anonymize.NewAnonymizer(anonymize.BasicProfile)

	mapping := anon.GetUIDMapping()
	if len(mapping) != 0 {
		t.Errorf("Expected empty UID mapping, got %d entries", len(mapping))
	}

	anon.ResetUIDMapping()
	mapping = anon.GetUIDMapping()
	if len(mapping) != 0 {
		t.Errorf("Expected empty UID mapping after reset, got %d entries", len(mapping))
	}
}

func TestProfileConstants(t *testing.T) {
	profiles := []anonymize.Profile{
		anonymize.BasicProfile,
		anonymize.CleanDescriptorsProfile,
		anonymize.CleanGraphicsProfile,
		anonymize.CleanStructuredContentProfile,
		anonymize.RetainLongFullDatesProfile,
		anonymize.RetainPatientCharsProfile,
		anonymize.RetainDeviceIdentProfile,
		anonymize.RetainUIDsProfile,
		anonymize.RetainSafePrivateProfile,
	}

	for i, p := range profiles {
		if int(p) != i {
			t.Errorf("Profile %d has unexpected value %d", i, int(p))
		}
	}
}

func TestActionConstants(t *testing.T) {
	actions := []anonymize.Action{
		anonymize.ActionRemove,
		anonymize.ActionEmpty,
		anonymize.ActionReplace,
		anonymize.ActionKeep,
		anonymize.ActionClean,
		anonymize.ActionUID,
		anonymize.ActionDummy,
	}

	for i, a := range actions {
		if int(a) != i {
			t.Errorf("Action %d has unexpected value %d", i, int(a))
		}
	}
}

func TestNewTemplate(t *testing.T) {
	tmpl := anonymize.NewTemplate("test", "test template")
	if tmpl == nil {
		t.Fatal("NewTemplate returned nil")
	}
	if tmpl.Name != "test" {
		t.Errorf("Expected name 'test', got %q", tmpl.Name)
	}
	if len(tmpl.Actions) != 0 {
		t.Errorf("Expected empty actions, got %d", len(tmpl.Actions))
	}
}

func TestTemplateSetAndRemoveAction(t *testing.T) {
	tmpl := anonymize.NewTemplate("test", "")
	patientName := tag.New(0x0010, 0x0010)

	tmpl.SetAction(patientName, anonymize.ActionRemove)
	if tmpl.Actions[patientName] != anonymize.ActionRemove {
		t.Error("Expected ActionRemove for PatientName")
	}

	tmpl.RemoveAction(patientName)
	if _, exists := tmpl.Actions[patientName]; exists {
		t.Error("Expected PatientName to be removed from template")
	}
}

func TestTemplateMerge(t *testing.T) {
	base := anonymize.NewTemplate("base", "")
	base.SetAction(tag.New(0x0010, 0x0010), anonymize.ActionRemove)
	base.SetAction(tag.New(0x0010, 0x0020), anonymize.ActionRemove)

	overlay := anonymize.NewTemplate("overlay", "")
	overlay.SetAction(tag.New(0x0010, 0x0010), anonymize.ActionKeep) // override

	base.Merge(overlay)
	if base.Actions[tag.New(0x0010, 0x0010)] != anonymize.ActionKeep {
		t.Error("Merge should override PatientName action")
	}
	if base.Actions[tag.New(0x0010, 0x0020)] != anonymize.ActionRemove {
		t.Error("Merge should preserve non-overlapping PatientID action")
	}
}

func TestTemplateClone(t *testing.T) {
	orig := anonymize.NewTemplate("orig", "original")
	orig.SetAction(tag.New(0x0010, 0x0010), anonymize.ActionRemove)

	cloned := orig.Clone()
	if cloned.Name != orig.Name {
		t.Error("Clone should preserve name")
	}

	// Modify clone, original should be unchanged
	cloned.SetAction(tag.New(0x0010, 0x0010), anonymize.ActionKeep)
	if orig.Actions[tag.New(0x0010, 0x0010)] != anonymize.ActionRemove {
		t.Error("Modifying clone should not affect original")
	}
}

func TestBasicProfileTemplate(t *testing.T) {
	tmpl := anonymize.BasicProfileTemplate()
	if tmpl == nil {
		t.Fatal("BasicProfileTemplate returned nil")
	}
	if len(tmpl.Actions) == 0 {
		t.Error("Basic profile template should have actions")
	}
	// Check a known action
	if tmpl.Actions[tag.New(0x0010, 0x0010)] != anonymize.ActionReplace {
		t.Error("PatientName should be ActionReplace in basic profile")
	}
}

func TestCleanDescriptorsTemplate(t *testing.T) {
	tmpl := anonymize.CleanDescriptorsTemplate()
	// StudyDescription should be ActionClean instead of ActionRemove
	if tmpl.Actions[tag.New(0x0008, 0x1030)] != anonymize.ActionClean {
		t.Error("StudyDescription should be ActionClean in clean descriptors template")
	}
}

func TestRetainDatesTemplate(t *testing.T) {
	tmpl := anonymize.RetainDatesTemplate()
	if tmpl.Actions[tag.New(0x0008, 0x0020)] != anonymize.ActionKeep {
		t.Error("StudyDate should be ActionKeep in retain dates template")
	}
}

func TestAnonymizerApplyTemplate(t *testing.T) {
	anon := anonymize.NewAnonymizer(anonymize.BasicProfile)
	tmpl := anonymize.NewTemplate("custom", "")
	tmpl.SetAction(tag.New(0x0010, 0x0010), anonymize.ActionKeep)

	anon.ApplyTemplate(tmpl)
	// Should not panic, template is applied
}

func TestAnonymizerSetTemplate(t *testing.T) {
	anon := anonymize.NewAnonymizer(anonymize.BasicProfile)
	tmpl := anonymize.RetainDatesTemplate()
	anon.SetTemplate(tmpl)
	// Should not panic
}
