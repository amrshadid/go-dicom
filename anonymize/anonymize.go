package anonymize

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Profile represents a DICOM de-identification profile as defined in PS3.15 Annex E.
type Profile int

const (
	// BasicProfile is the default de-identification profile per DICOM PS3.15 Table E.1-1.
	BasicProfile Profile = iota
	// CleanDescriptorsProfile removes or cleans description fields that may contain PHI.
	CleanDescriptorsProfile
	// CleanGraphicsProfile removes graphical data that may contain burned-in PHI.
	CleanGraphicsProfile
	// CleanStructuredContentProfile cleans structured content that may contain PHI.
	CleanStructuredContentProfile
	// RetainLongFullDatesProfile retains full dates (not just year) in the output.
	RetainLongFullDatesProfile
	// RetainPatientCharsProfile retains patient characteristic attributes (age, sex, size, weight).
	RetainPatientCharsProfile
	// RetainDeviceIdentProfile retains device identity attributes.
	RetainDeviceIdentProfile
	// RetainUIDsProfile retains original UIDs without remapping.
	RetainUIDsProfile
	// RetainSafePrivateProfile retains private attributes known to be safe.
	RetainSafePrivateProfile
)

// Action represents a de-identification action to apply to a DICOM data element.
type Action int

const (
	// ActionRemove deletes the element entirely (X action in PS3.15).
	ActionRemove Action = iota
	// ActionEmpty replaces the value with a zero-length value (Z action in PS3.15).
	ActionEmpty
	// ActionReplace replaces the value with a substitute value.
	ActionReplace
	// ActionKeep keeps the element unchanged (K action in PS3.15).
	ActionKeep
	// ActionClean cleans the value in a context-dependent manner (C action in PS3.15).
	ActionClean
	// ActionUID replaces UID values with consistently remapped new UIDs (U action in PS3.15).
	ActionUID
	// ActionDummy replaces the value with a default/dummy value (D action in PS3.15).
	ActionDummy
)

// Anonymizer performs DICOM de-identification on datasets according to a
// configured profile and optional per-tag action overrides.
type Anonymizer struct {
	profile           Profile
	retainPatientName bool
	retainPatientID   bool
	customActions     map[tag.Tag]Action
	uidMap            map[string]string // maps original UIDs to replacement UIDs
	mu                sync.RWMutex
}

// NewAnonymizer creates a new Anonymizer with the specified de-identification profile.
func NewAnonymizer(profile Profile) *Anonymizer {
	return &Anonymizer{
		profile:       profile,
		customActions: make(map[tag.Tag]Action),
		uidMap:        make(map[string]string),
	}
}

// SetRetainPatientName controls whether the patient name is retained (true)
// or replaced with "ANONYMOUS" (false, the default).
func (a *Anonymizer) SetRetainPatientName(retain bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.retainPatientName = retain
}

// SetRetainPatientID controls whether the patient ID is retained (true)
// or replaced with a random identifier (false, the default).
func (a *Anonymizer) SetRetainPatientID(retain bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.retainPatientID = retain
}

// SetCustomAction sets a custom de-identification action for a specific tag,
// overriding the action defined by the current profile.
func (a *Anonymizer) SetCustomAction(t tag.Tag, action Action) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.customActions[t] = action
}

// GetUIDMapping returns a copy of the current UID remapping table. The keys
// are original UIDs and the values are the replacement UIDs.
func (a *Anonymizer) GetUIDMapping() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[string]string, len(a.uidMap))
	for k, v := range a.uidMap {
		result[k] = v
	}
	return result
}

// ResetUIDMapping clears the UID remapping table. This should be called
// when starting anonymization of a new patient to avoid UID collisions.
func (a *Anonymizer) ResetUIDMapping() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.uidMap = make(map[string]string)
}

// Anonymize applies de-identification to all applicable elements in the dataset
// according to the configured profile and any custom action overrides.
func (a *Anonymizer) Anonymize(ds *dataset.Dataset) error {
	if ds == nil {
		return fmt.Errorf("dataset is nil")
	}
	return a.anonymizeDataset(ds, 0)
}

// maxAnonymizeDepth bounds recursion into nested sequences, which a crafted
// file could otherwise make unbounded.
const maxAnonymizeDepth = 64

// anonymizeDataset applies the profile to one data set and everything nested
// inside it.
//
// This walks the data set's own tags rather than the profile's. Iterating the
// profile finds only what it can name and only at the top level, which missed
// two whole classes of attribute: the ones named by a range of groups — curve
// and overlay data, where a burned-in name most often survives — and every
// attribute inside a sequence. Patient identifiers appear inside Request
// Attributes Sequence and Referenced Image Sequence in ordinary files, and were
// left untouched.
func (a *Anonymizer) anonymizeDataset(ds *dataset.Dataset, depth int) error {
	if depth > maxAnonymizeDepth {
		return fmt.Errorf("anonymize: sequences nest deeper than %d levels", maxAnonymizeDepth)
	}

	// Collected first: applying an action may remove an element, and mutating
	// the data set while ranging over its tags is not safe.
	tags := ds.Tags()

	for _, t := range tags {
		elem, ok := ds.Get(t)
		if !ok {
			continue
		}
		if seq, isSeq := elem.GetValue().(*sequence.Sequence); isSeq {
			// A sequence holds data sets, not a value, so most actions cannot
			// be applied to it directly — and one of them used to be applied by
			// doing nothing at all. The profile marks Source Image Sequence
			// X/Z/U*, whose asterisk means "replace the instance UIDs contained
			// in it". The UID action reached a sequence, matched neither of the
			// value types it knew, and returned. Every referenced UID inside
			// survived, in 18 of pydicom's 69 files, each one linking the
			// de-identified object straight back to the instance it came from.
			switch a.getDefaultAction(t) {
			case ActionRemove:
				ds.Remove(t)
			case ActionEmpty:
				// Emptying a sequence means no items, not an empty value.
				for seq.Length() > 0 {
					if err := seq.Remove(0); err != nil {
						break
					}
				}
			default:
				for i := 0; i < seq.Length(); i++ {
					item, err := seq.Get(i)
					if err != nil {
						continue
					}
					inner, isDS := item.(*dataset.Dataset)
					if !isDS {
						continue
					}
					if err := a.anonymizeDataset(inner, depth+1); err != nil {
						return err
					}
				}
			}
			continue
		}
		if err := a.AnonymizeElement(ds, t); err != nil {
			return fmt.Errorf("anonymizing tag %s: %w", t.String(), err)
		}
	}

	// Private tags carry whatever their vendor put there, which the profile
	// cannot describe and cannot vouch for.
	if a.profile != RetainSafePrivateProfile {
		for _, t := range ds.Tags() {
			if t.IsPrivate() {
				ds.Remove(t)
			}
		}
	}

	return nil
}

// AnonymizeElement applies the appropriate de-identification action to a
// single element identified by tag within the dataset.
func (a *Anonymizer) AnonymizeElement(ds *dataset.Dataset, t tag.Tag) error {
	if ds == nil {
		return fmt.Errorf("dataset is nil")
	}

	action := a.getDefaultAction(t)
	return a.applyAction(ds, t, action)
}

// getDefaultAction determines the action for a tag based on the current profile,
// custom overrides, and the basic profile actions table.
func (a *Anonymizer) getDefaultAction(t tag.Tag) Action {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Custom actions take highest priority
	if action, ok := a.customActions[t]; ok {
		return action
	}

	// Profile-specific overrides
	switch a.profile {
	case RetainUIDsProfile:
		if action, ok := basicProfileActions[t]; ok && action == ActionUID {
			return ActionKeep
		}
	case RetainPatientCharsProfile:
		// Keep patient characteristic attributes
		switch t {
		case tag.New(0x0010, 0x0040), // PatientSex
			tag.New(0x0010, 0x1010), // PatientAge
			tag.New(0x0010, 0x1020), // PatientSize
			tag.New(0x0010, 0x1030): // PatientWeight
			return ActionKeep
		}
	case RetainDeviceIdentProfile:
		// Keep device identification attributes
		switch t {
		case tag.New(0x0008, 0x0070), // Manufacturer
			tag.New(0x0008, 0x1090), // ManufacturerModelName
			tag.New(0x0018, 0x1000), // DeviceSerialNumber
			tag.New(0x0018, 0x1020), // SoftwareVersions
			tag.New(0x0008, 0x1010): // StationName
			return ActionKeep
		}
	case CleanDescriptorsProfile:
		// Clean description fields instead of removing them
		switch t {
		case tag.New(0x0008, 0x1030), // StudyDescription
			tag.New(0x0008, 0x103E): // SeriesDescription
			return ActionClean
		}
	case RetainLongFullDatesProfile:
		// Keep full date/time values
		switch t {
		case tag.New(0x0008, 0x0020), // StudyDate
			tag.New(0x0008, 0x0030): // StudyTime
			return ActionKeep
		}
	}

	// Handle retain flags
	if a.retainPatientName && t == tag.New(0x0010, 0x0010) {
		return ActionKeep
	}
	if a.retainPatientID && t == tag.New(0x0010, 0x0020) {
		return ActionKeep
	}

	// The standard first. The hand-written table this used to consult held
	// thirty-eight attributes, and four of them said to keep Patient's Sex,
	// Age, Size and Weight — which is what RetainPatientCharsProfile is for,
	// and which the Basic Profile is precisely not. Consulted first, those four
	// entries kept the attributes in every profile, including the one whose
	// whole purpose is to remove them.
	if action, ok := ps315BasicProfile[t]; ok {
		return action
	}
	if action, ok := basicProfileActions[t]; ok {
		return action
	}
	// Curve and overlay data are named by a range of groups rather than by one
	// tag, so they cannot be looked up.
	for _, rule := range ps315RepeatingGroups {
		if rule.matches(t) {
			return rule.action
		}
	}

	// An attribute the profile does not name is left alone. The profile is a
	// floor, and discarding data it did not ask about is its own kind of wrong.
	return ActionKeep
}

// applyAction applies a single de-identification action to an element in the dataset.
func (a *Anonymizer) applyAction(ds *dataset.Dataset, t tag.Tag, action Action) error {
	switch action {
	case ActionRemove:
		ds.Remove(t)

	case ActionEmpty:
		if err := ds.SetValue(t, []byte{}); err != nil {
			return fmt.Errorf("setting empty value for %s: %w", t.String(), err)
		}

	case ActionReplace:
		replacement := a.getReplacementValue(t)
		if err := ds.SetValue(t, []byte(replacement)); err != nil {
			return fmt.Errorf("replacing value for %s: %w", t.String(), err)
		}

	case ActionKeep:
		// No action needed

	case ActionClean:
		// Clean by replacing with a generic description
		if err := ds.SetValue(t, []byte("CLEANED")); err != nil {
			return fmt.Errorf("cleaning value for %s: %w", t.String(), err)
		}

	case ActionUID:
		elem, exists := ds.Get(t)
		if !exists {
			return nil
		}
		value := elem.GetValue()
		var originalUID string
		switch v := value.(type) {
		case []byte:
			originalUID = strings.TrimRight(string(v), "\x00 ")
		case string:
			originalUID = strings.TrimRight(v, "\x00 ")
		default:
			return nil
		}
		if originalUID == "" {
			return nil
		}
		newUID := a.generateNewUID(originalUID)
		if err := ds.SetValue(t, []byte(newUID)); err != nil {
			return fmt.Errorf("replacing UID for %s: %w", t.String(), err)
		}

	case ActionDummy:
		replacement := a.getDummyValue(t)
		if err := ds.SetValue(t, []byte(replacement)); err != nil {
			return fmt.Errorf("setting dummy value for %s: %w", t.String(), err)
		}

	default:
		return fmt.Errorf("unknown action %d for tag %s", action, t.String())
	}

	return nil
}

// getReplacementValue returns an appropriate replacement value for a tag.
func (a *Anonymizer) getReplacementValue(t tag.Tag) string {
	switch t {
	case tag.New(0x0010, 0x0010): // PatientName
		return "ANONYMOUS"
	case tag.New(0x0010, 0x0020): // PatientID
		return generateAnonymousID()
	default:
		return "ANONYMIZED"
	}
}

// getDummyValue returns a default dummy value for a tag based on its VR.
func (a *Anonymizer) getDummyValue(t tag.Tag) string {
	vr := t.GetVR()
	switch vr {
	case "DA":
		return "19000101"
	case "TM":
		return "000000"
	case "DT":
		return "19000101000000"
	case "PN":
		return "ANONYMOUS"
	default:
		return ""
	}
}

// generateAnonymousID generates a random anonymous patient ID.
func generateAnonymousID() string {
	// Use crypto/rand for unpredictable IDs
	const idLength = 12
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, idLength)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			// Fallback to timestamp-based ID on error
			return fmt.Sprintf("ANON%d", time.Now().UnixNano())
		}
		result[i] = charset[n.Int64()]
	}
	return string(result)
}

// generateNewUID generates a new UID for an original UID, maintaining consistent
// mapping so the same original UID always maps to the same new UID within a session.
func (a *Anonymizer) generateNewUID(originalUID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Return existing mapping if present
	if newUID, ok := a.uidMap[originalUID]; ok {
		return newUID
	}

	// Generate a new UID preserving the DICOM UID root prefix.
	// Use the standard "2.25." prefix followed by a random UUID-derived number
	// as recommended by DICOM for implementation-generated UIDs.
	prefix := "2.25."

	// If the original UID has a recognizable root, try to preserve it
	parts := strings.SplitN(originalUID, ".", 4)
	if len(parts) >= 3 {
		prefix = parts[0] + "." + parts[1] + "." + parts[2] + "."
	}

	// Generate random suffix using crypto/rand
	suffix := generateUIDSuffix()
	newUID := prefix + suffix

	// Ensure UID does not exceed 64 characters (DICOM max)
	if len(newUID) > 64 {
		newUID = newUID[:64]
	}

	a.uidMap[originalUID] = newUID
	return newUID
}

// generateUIDSuffix generates a random numeric suffix for UID generation.
func generateUIDSuffix() string {
	// Generate a large random number for the suffix
	max := new(big.Int)
	max.SetString("999999999999999999", 10)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		// Fallback to timestamp
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return n.String()
}

// Template represents a reusable de-identification template that defines
// actions for a set of tags. Templates can be combined, saved, and loaded
// to create consistent anonymization configurations across projects.
type Template struct {
	Name        string
	Description string
	Actions     map[tag.Tag]Action
}

// NewTemplate creates a new empty de-identification template.
func NewTemplate(name, description string) *Template {
	return &Template{
		Name:        name,
		Description: description,
		Actions:     make(map[tag.Tag]Action),
	}
}

// SetAction adds or updates an action for a tag in the template.
func (tmpl *Template) SetAction(t tag.Tag, action Action) {
	tmpl.Actions[t] = action
}

// RemoveAction removes a tag's action from the template.
func (tmpl *Template) RemoveAction(t tag.Tag) {
	delete(tmpl.Actions, t)
}

// Merge combines another template into this one. Actions from the other
// template override actions in this template for the same tag.
func (tmpl *Template) Merge(other *Template) {
	for t, action := range other.Actions {
		tmpl.Actions[t] = action
	}
}

// Clone creates a deep copy of the template.
func (tmpl *Template) Clone() *Template {
	cloned := NewTemplate(tmpl.Name, tmpl.Description)
	for t, action := range tmpl.Actions {
		cloned.Actions[t] = action
	}
	return cloned
}

// BasicProfileTemplate returns a template based on the DICOM PS3.15
// Annex E Basic Application Level Confidentiality Profile.
func BasicProfileTemplate() *Template {
	tmpl := NewTemplate("Basic Profile", "DICOM PS3.15 Annex E Basic Application Level Confidentiality Profile")
	for t, action := range basicProfileActions {
		tmpl.Actions[t] = action
	}
	return tmpl
}

// CleanDescriptorsTemplate returns a template that cleans description
// fields instead of removing them.
func CleanDescriptorsTemplate() *Template {
	tmpl := BasicProfileTemplate()
	tmpl.Name = "Clean Descriptors"
	tmpl.Description = "Basic profile with description fields cleaned instead of removed"
	tmpl.Actions[tag.New(0x0008, 0x1030)] = ActionClean // StudyDescription
	tmpl.Actions[tag.New(0x0008, 0x103E)] = ActionClean // SeriesDescription
	return tmpl
}

// RetainDatesTemplate returns a template that retains full date/time values.
func RetainDatesTemplate() *Template {
	tmpl := BasicProfileTemplate()
	tmpl.Name = "Retain Dates"
	tmpl.Description = "Basic profile with full dates retained for longitudinal studies"
	tmpl.Actions[tag.New(0x0008, 0x0020)] = ActionKeep // StudyDate
	tmpl.Actions[tag.New(0x0008, 0x0030)] = ActionKeep // StudyTime
	return tmpl
}

// RetainDeviceTemplate returns a template that retains device identity attributes.
func RetainDeviceTemplate() *Template {
	tmpl := BasicProfileTemplate()
	tmpl.Name = "Retain Device Identity"
	tmpl.Description = "Basic profile with device identification retained"
	tmpl.Actions[tag.New(0x0018, 0x1000)] = ActionKeep // DeviceSerialNumber
	tmpl.Actions[tag.New(0x0008, 0x1010)] = ActionKeep // StationName
	return tmpl
}

// SetTemplate configures the anonymizer to use a custom template.
// The template's actions override the basic profile for matching tags.
// Custom per-tag actions set via SetCustomAction still take highest priority.
func (a *Anonymizer) SetTemplate(tmpl *Template) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Merge template actions into custom actions (template has lower priority
	// than existing custom actions, so only set if not already customized)
	for t, action := range tmpl.Actions {
		if _, exists := a.customActions[t]; !exists {
			a.customActions[t] = action
		}
	}
}

// ApplyTemplate applies a template, replacing all existing custom actions.
func (a *Anonymizer) ApplyTemplate(tmpl *Template) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.customActions = make(map[tag.Tag]Action, len(tmpl.Actions))
	for t, action := range tmpl.Actions {
		a.customActions[t] = action
	}
}

// basicProfileActions defines the de-identification actions for the DICOM PS3.15
// Annex E Basic Application Level Confidentiality Profile.
var basicProfileActions = map[tag.Tag]Action{
	// Patient-related tags
	tag.New(0x0010, 0x0010): ActionReplace, // PatientName -> replace with "ANONYMOUS"
	tag.New(0x0010, 0x0020): ActionReplace, // PatientID -> replace with random ID
	tag.New(0x0010, 0x0030): ActionEmpty,   // PatientBirthDate
	tag.New(0x0010, 0x0040): ActionKeep,    // PatientSex (usually kept in basic profile)
	tag.New(0x0010, 0x1000): ActionRemove,  // OtherPatientIDs
	tag.New(0x0010, 0x1001): ActionRemove,  // OtherPatientNames
	tag.New(0x0010, 0x1010): ActionKeep,    // PatientAge
	tag.New(0x0010, 0x1020): ActionKeep,    // PatientSize
	tag.New(0x0010, 0x1030): ActionKeep,    // PatientWeight
	tag.New(0x0010, 0x2160): ActionRemove,  // EthnicGroup
	tag.New(0x0010, 0x21B0): ActionRemove,  // AdditionalPatientHistory
	tag.New(0x0010, 0x4000): ActionRemove,  // PatientComments

	// Study-related tags
	tag.New(0x0008, 0x0020): ActionEmpty,  // StudyDate
	tag.New(0x0008, 0x0030): ActionEmpty,  // StudyTime
	tag.New(0x0008, 0x0050): ActionEmpty,  // AccessionNumber
	tag.New(0x0008, 0x0080): ActionRemove, // InstitutionName
	tag.New(0x0008, 0x0081): ActionRemove, // InstitutionAddress
	tag.New(0x0008, 0x0090): ActionEmpty,  // ReferringPhysicianName
	tag.New(0x0008, 0x0096): ActionRemove, // ReferringPhysicianIdentificationSequence
	tag.New(0x0008, 0x1010): ActionRemove, // StationName
	tag.New(0x0008, 0x1030): ActionRemove, // StudyDescription
	tag.New(0x0008, 0x103E): ActionRemove, // SeriesDescription
	tag.New(0x0008, 0x1040): ActionRemove, // InstitutionalDepartmentName
	tag.New(0x0008, 0x1048): ActionRemove, // PhysiciansOfRecord
	tag.New(0x0008, 0x1050): ActionRemove, // PerformingPhysicianName
	tag.New(0x0008, 0x1060): ActionRemove, // NameOfPhysiciansReadingStudy
	tag.New(0x0008, 0x1070): ActionRemove, // OperatorsName
	tag.New(0x0020, 0x0010): ActionEmpty,  // StudyID
	tag.New(0x0020, 0x4000): ActionRemove, // ImageComments

	// UID-related tags
	tag.New(0x0008, 0x0018): ActionUID,  // SOPInstanceUID
	tag.New(0x0020, 0x000D): ActionUID,  // StudyInstanceUID
	tag.New(0x0020, 0x000E): ActionUID,  // SeriesInstanceUID
	tag.New(0x0008, 0x0016): ActionKeep, // SOPClassUID (must be kept)
	tag.New(0x0002, 0x0003): ActionUID,  // MediaStorageSOPInstanceUID

	// Device-related tags
	tag.New(0x0008, 0x0070): ActionKeep,   // Manufacturer (usually kept)
	tag.New(0x0008, 0x1090): ActionKeep,   // ManufacturerModelName
	tag.New(0x0018, 0x1000): ActionRemove, // DeviceSerialNumber
	tag.New(0x0018, 0x1020): ActionKeep,   // SoftwareVersions
}
