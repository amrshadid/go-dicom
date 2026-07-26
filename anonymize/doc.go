// Package anonymize removes protected health information from DICOM data sets,
// implementing the de-identification profiles defined in DICOM PS3.15 Annex E.
//
// De-identification is not one operation. PS3.15 defines a Basic Application
// Level Confidentiality Profile that removes or replaces the attributes known
// to carry identity, plus option profiles that retain specific categories a
// study still needs — longitudinal dates, patient characteristics, device
// identity. This package models both.
//
// # Quick start
//
//	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
//	if err := a.Anonymize(ds); err != nil {
//		log.Fatal(err)
//	}
//
// Anonymize mutates the data set in place. Clone it first if the original is
// still needed.
//
// # Profiles
//
// The profile selects which PS3.15 option applies on top of the basic profile:
//
//   - [BasicProfile] — the Basic Application Level Confidentiality Profile.
//     Removes or replaces every attribute in PS3.15 Table E.1-1.
//   - [CleanDescriptorsProfile] — also cleans free-text description fields,
//     which carry identity in practice even though their definition does not
//     require it.
//   - [CleanGraphicsProfile] — also removes graphic annotation that may contain
//     burned-in identity.
//   - [CleanStructuredContentProfile] — also cleans structured report content.
//   - [RetainLongFullDatesProfile] — keeps full dates rather than reducing them
//     to a year. Required where the interval between studies matters.
//   - [RetainPatientCharsProfile] — keeps age, sex, size, and weight, often
//     needed for dosimetry or normalization.
//   - [RetainDeviceIdentProfile] — keeps device identity, for studies analyzing
//     scanner behavior.
//   - [RetainUIDsProfile] — keeps the original UIDs instead of remapping them.
//   - [RetainSafePrivateProfile] — keeps private attributes known to be safe.
//
// # Actions
//
// Each attribute is handled by one action, corresponding to the codes in the
// PS3.15 tables:
//
//   - [ActionRemove] — delete the attribute entirely
//   - [ActionEmpty] — keep the attribute, replace the value with zero length
//   - [ActionReplace] — replace with a non-identifying value of the same type
//   - [ActionKeep] — leave unchanged
//   - [ActionClean] — replace while retaining meaning where possible
//   - [ActionUID] — replace with a consistently remapped UID
//   - [ActionDummy] — replace with a fixed dummy value
//
// Most actions replace rather than delete, so the attribute stays present for
// readers that require it. The basic profile leaves PatientName in place with a
// dummy value rather than removing it.
//
// Override any single tag with [Anonymizer.SetCustomAction], which is how site
// policy is expressed on top of the standard profile:
//
//	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
//	// This site treats the accession number as non-identifying.
//	a.SetCustomAction(tag.New(0x0008, 0x0050), anonymize.ActionKeep)
//	// A vendor private tag known to carry an operator name.
//	a.SetCustomAction(tag.New(0x0029, 0x1010), anonymize.ActionRemove)
//
// [Anonymizer.SetRetainPatientName] and [Anonymizer.SetRetainPatientID] are
// shorthands for the two most commonly overridden attributes.
//
// # UID remapping
//
// Study, series, and SOP Instance UIDs identify a patient across data sets even
// after names are gone, so [ActionUID] replaces them. The replacement is
// consistent within an Anonymizer: the same input UID always maps to the same
// output, which preserves the study/series/instance hierarchy across every file
// in a study.
//
// That mapping lives in the Anonymizer, so use one instance for a whole study:
//
//	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
//	for _, ds := range studyDatasets {
//		if err := a.Anonymize(ds); err != nil {
//			return err
//		}
//	}
//	// The relationships between the data sets are intact.
//
// Creating a new Anonymizer per file breaks those relationships, because each
// one generates a fresh mapping.
//
// [Anonymizer.GetUIDMapping] returns the accumulated mapping. It is the link
// back to the original identities: treat it as sensitive, store it apart from
// the anonymized data, or discard it. [Anonymizer.ResetUIDMapping] clears it
// when starting an unrelated batch.
//
// # Templates
//
// A [Template] supplies the replacement values used by [ActionReplace] and
// [ActionDummy] — patient name, ID, dates, institution. Set one to make output
// deterministic across runs, or to make anonymized data recognizable in a test
// corpus.
//
//	a.SetTemplate(&anonymize.Template{
//		PatientName: "ANON^SUBJECT-014",
//		PatientID:   "STUDY-014",
//	})
//
// # What this package cannot reach
//
// De-identification is a property of the whole data set, not of a tag list.
// This package operates on DICOM attributes; identity that lives elsewhere
// survives it:
//
//   - Burned-in annotation in pixel data. Text rendered into the image is not
//     an attribute. Check BurnedInAnnotation (0028,0301), and treat ultrasound,
//     screen captures, and secondary captures as suspect.
//   - Private tags that were deliberately retained. The basic profile removes
//     private attributes outside the safe list, but one kept on purpose may
//     carry identity.
//   - Free text in reports and comments, unless a cleaning profile is used —
//     and cleaning is heuristic.
//   - Identity inferable from the data itself: a facial reconstruction from a
//     head CT, or a rare diagnosis in a small cohort.
//
// Removing attributes reduces risk; it does not by itself make data
// non-identifiable under HIPAA, GDPR, or any other regime. Validate output
// against your own policy before release. See SECURITY.md in the repository
// root for the project's guidance on handling protected health information.
package anonymize
