package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// AnonymizeCommand de-identifies DICOM files with the PS3.15 Annex E profiles.
type AnonymizeCommand struct {
	profile       string
	uidMap        string
	redact        regionList
	allowBurnedIn bool
	help          bool
}

// regionList collects each -redact x,y,w,h.
type regionList []dataset.PixelRegion

func (r *regionList) String() string { return fmt.Sprint(len(*r), " region(s)") }

func (r *regionList) Set(value string) error {
	var region dataset.PixelRegion
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return fmt.Errorf("a region is x,y,width,height")
	}
	for i, field := range []*int{&region.X, &region.Y, &region.Width, &region.Height} {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			return fmt.Errorf("a region is x,y,width,height in whole pixels")
		}
		*field = n
	}
	if region.Width <= 0 || region.Height <= 0 {
		return fmt.Errorf("a region needs a width and a height above zero")
	}
	*r = append(*r, region)
	return nil
}

// anonymizeProfiles names the option each -profile value applies on top of the
// Basic Profile.
var anonymizeProfiles = map[string]anonymize.Profile{
	"basic":                          anonymize.BasicProfile,
	"clean-descriptors":              anonymize.CleanDescriptorsProfile,
	"clean-graphics":                 anonymize.CleanGraphicsProfile,
	"clean-structured-content":       anonymize.CleanStructuredContentProfile,
	"retain-dates":                   anonymize.RetainLongFullDatesProfile,
	"retain-patient-characteristics": anonymize.RetainPatientCharsProfile,
	"retain-device-identity":         anonymize.RetainDeviceIdentProfile,
	"retain-uids":                    anonymize.RetainUIDsProfile,
	"retain-safe-private":            anonymize.RetainSafePrivateProfile,
}

func profileNames() string {
	names := make([]string, 0, len(anonymizeProfiles))
	for name := range anonymizeProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// Name returns the command name.
func (ac *AnonymizeCommand) Name() string {
	return "anonymize"
}

// Description returns the command description.
func (ac *AnonymizeCommand) Description() string {
	return "De-identify a DICOM file or directory (PS3.15 Annex E)"
}

// AddFlags adds command flags to the flag set.
func (ac *AnonymizeCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&ac.help, "h", false, "Show help for this command")
	fs.BoolVar(&ac.help, "help", false, "Show help for this command")
	fs.StringVar(&ac.profile, "profile", "basic", "Profile: "+profileNames())
	fs.StringVar(&ac.uidMap, "uid-map", "", "Write the original-to-new UID mapping to this file as JSON")
	fs.Var(&ac.redact, "redact", "Fill the rectangle x,y,width,height with black in every frame (repeatable)")
	fs.BoolVar(&ac.allowBurnedIn, "allow-burned-in", false, "Write images that may hold burned-in text without -redact")
}

// Execute runs the anonymize command.
func (ac *AnonymizeCommand) Execute(args []string) error {
	flags := flag.NewFlagSet("anonymize", flag.ContinueOnError)
	ac.AddFlags(flags)
	positional, err := ParseArgs(flags, args)
	if err != nil {
		return fmt.Errorf("anonymize: failed to parse flags: %v", err)
	}
	if ac.help {
		ac.showHelp()
		return nil
	}
	if len(positional) != 2 {
		return fmt.Errorf("anonymize: expected an input and an output\nRun 'go-dicom anonymize -h' for usage")
	}
	profile, ok := anonymizeProfiles[ac.profile]
	if !ok {
		return fmt.Errorf("anonymize: unknown profile %q; the profiles are %s", ac.profile, profileNames())
	}

	input, output := positional[0], positional[1]
	info, err := os.Stat(input)
	if err != nil {
		return fmt.Errorf("anonymize: %w", err)
	}

	// One Anonymizer for the whole run: its UID mapping is what keeps the
	// instances of a study in one study once their UIDs are replaced.
	anonymizer := anonymize.NewAnonymizer(profile)

	var done, skipped, failed int
	if info.IsDir() {
		done, skipped, failed, err = ac.anonymizeDirectory(anonymizer, input, output)
		if err != nil {
			return err
		}
	} else {
		// An existing directory as the output takes the file under its own name.
		if out, statErr := os.Stat(output); statErr == nil && out.IsDir() {
			output = filepath.Join(output, filepath.Base(input))
		}
		if sameFile(input, output) {
			return fmt.Errorf("anonymize: the output is the input; the original would be lost")
		}
		if err := ac.anonymizeFile(anonymizer, input, output); err != nil {
			return fmt.Errorf("anonymize: %s: %w", input, err)
		}
		done = 1
	}

	if ac.uidMap != "" {
		if err := writeUIDMap(ac.uidMap, anonymizer.GetUIDMapping()); err != nil {
			return fmt.Errorf("anonymize: %w", err)
		}
	}

	fmt.Printf("De-identified %d file(s)", done)
	if skipped > 0 {
		fmt.Printf(", skipped %d", skipped)
	}
	fmt.Println()
	if failed > 0 {
		return fmt.Errorf("anonymize: %d file(s) failed and were not written", failed)
	}
	return nil
}

// anonymizeDirectory walks input and writes each DICOM file under output at
// the same relative path.
func (ac *AnonymizeCommand) anonymizeDirectory(anonymizer *anonymize.Anonymizer, input, output string) (done, skipped, failed int, err error) {
	inAbs, err := filepath.Abs(input)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("anonymize: %w", err)
	}
	outAbs, err := filepath.Abs(output)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("anonymize: %w", err)
	}
	// Inside the input, the output would be walked as input, and at the input
	// it would replace the originals.
	if rel, relErr := filepath.Rel(inAbs, outAbs); relErr == nil && !strings.HasPrefix(rel, "..") {
		return 0, 0, 0, fmt.Errorf("anonymize: the output directory is the input or inside it")
	}

	walkErr := filepath.WalkDir(inAbs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(inAbs, path)
		if err != nil {
			return err
		}
		var skip skipError
		switch err := ac.anonymizeFile(anonymizer, path, filepath.Join(outAbs, rel)); {
		case err == nil:
			done++
		case errors.As(err, &skip):
			// Not copied either: whatever it holds was not de-identified.
			fmt.Fprintf(os.Stderr, "skipped %s: %v\n", rel, err)
			skipped++
		default:
			fmt.Fprintf(os.Stderr, "failed %s: %v\n", rel, err)
			failed++
		}
		return nil
	})
	if walkErr != nil {
		return done, skipped, failed, fmt.Errorf("anonymize: %w", walkErr)
	}
	return done, skipped, failed, nil
}

// skipError is why a file is left out of the output without the run failing.
type skipError string

func (e skipError) Error() string { return string(e) }

const (
	errNotDICOM skipError = "not a DICOM file"
	// A DICOMDIR's records name every patient, and its offsets would not
	// survive being rewritten. It is regenerated, not de-identified.
	errDICOMDIR skipError = "a DICOMDIR is not de-identified; generate a new one over the output"
)

// mediaStorageDirectory is the SOP class of a DICOMDIR.
const mediaStorageDirectory = "1.2.840.10008.1.3.10"

// anonymizeFile reads input, de-identifies it and writes it to output.
func (ac *AnonymizeCommand) anonymizeFile(anonymizer *anonymize.Anonymizer, input, output string) error {
	in, err := os.Open(input)
	if err != nil {
		return err
	}
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(in))
	_ = in.Close()
	if err != nil {
		return errNotDICOM
	}
	ds := df.GetDataset()

	// A file with no preamble is read as a bare data set, and so is anything
	// else that happens to parse. Without a SOP Class and Instance UID it is
	// not an instance.
	sopClass := datasetText(ds, tag.New(0x0008, 0x0016))
	if sopClass == "" && df.FileMetaInfo != nil {
		sopClass = df.FileMetaInfo.MediaStorageSOPClassUID
	}
	if sopClass == "" || datasetText(ds, tag.New(0x0008, 0x0018)) == "" {
		if sopClass == mediaStorageDirectory {
			return errDICOMDIR
		}
		return errNotDICOM
	}

	// The pixels before the attributes: an image that cannot be cleaned, or
	// that may hold text nobody has answered for, is not written at all.
	if _, hasPixels := ds.Get(tag.New(0x7FE0, 0x0010)); hasPixels {
		switch {
		case len(ac.redact) > 0:
			if err := anonymizer.CleanPixelData(ds, ac.redact...); err != nil {
				return err
			}
		case !ac.allowBurnedIn:
			if risk, reason := anonymize.BurnedInRisk(ds); risk {
				return fmt.Errorf("it may hold text burned into the pixels: %s. "+
					"Give -redact x,y,width,height for where the text is, "+
					"or -allow-burned-in if there is none", reason)
			}
		}
	}

	if err := anonymizer.Anonymize(ds); err != nil {
		return err
	}

	// The data set's new UID, not the meta's old one remapped: the two differ
	// in some source files, and the file meta has to name the instance it holds.
	instance := datasetText(ds, tag.New(0x0008, 0x0018))

	// The syntax the file was read in. Encapsulated pixel data must keep it,
	// and nothing else needs to change, unless redacting decompressed the
	// image, which is then native and written so.
	syntax := ""
	if df.FileMetaInfo != nil {
		syntax = df.FileMetaInfo.TransferSyntaxUID
	}
	if syntax == "" || compress.IsEncapsulated(syntax) != compress.IsEncapsulated(ds.TransferSyntaxUID()) {
		syntax = filewriter.StorageTransferSyntax(ds)
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	out, err := os.Create(output)
	if err != nil {
		return err
	}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    sopClass,
		MediaStorageSOPInstanceUID: instance,
		TransferSyntaxUID:          syntax,
	})
	err = w.AddDataElements(filewriter.ElementsFromDataset(ds))
	if err == nil {
		err = w.Write()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		// A partial file would pass for a de-identified one.
		_ = os.Remove(output)
		return err
	}
	return nil
}

// datasetText reads a text value without its padding.
func datasetText(ds *dataset.Dataset, t tag.Tag) string {
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	switch v := elem.GetValue().(type) {
	case []byte:
		return strings.TrimRight(string(v), " \x00")
	case string:
		return strings.TrimRight(v, " \x00")
	}
	return ""
}

// sameFile reports whether two paths name one file, existing or not.
func sameFile(a, b string) bool {
	if x, err := os.Stat(a); err == nil {
		if y, err := os.Stat(b); err == nil {
			return os.SameFile(x, y)
		}
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}

// writeUIDMap writes the mapping readable by its owner alone: it is the link
// from the de-identified data back to the original.
func writeUIDMap(path string, mapping map[string]string) error {
	data, err := json.MarshalIndent(mapping, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func (ac *AnonymizeCommand) showHelp() {
	printHelp(`De-identify a DICOM file or directory

USAGE:
  go-dicom anonymize [options] <input> <output>

  <input> is a file or a directory. A directory is walked, and each DICOM file
  is written under <output> at the same relative path. The input is never
  changed.

OPTIONS:
  -profile <name>    The PS3.15 Annex E option applied on top of the Basic
                     Profile (default: basic):
                       basic, clean-descriptors, clean-graphics,
                       clean-structured-content, retain-dates,
                       retain-patient-characteristics, retain-device-identity,
                       retain-uids, retain-safe-private
  -uid-map <file>    Write the original-to-new UID mapping as JSON. It links
                     the output back to the patient: keep it apart from the
                     output, or do not write it.
  -redact x,y,w,h    Fill a rectangle with black in every frame of every
                     image, counted in pixels from the top left. Repeat it
                     for more rectangles. Sets Burned In Annotation to NO and
                     records the Clean Pixel Data Option.
  -allow-burned-in   Write images that may hold burned-in text as they are.
  -h, -help          Show this help message

WHAT IT DOES:
  Removes or replaces the attributes PS3.15 Table E.1-1 lists, replaces UIDs
  consistently across the run so a study stays one study, sets Patient
  Identity Removed, and records the method used.

BURNED-IN TEXT:
  Ultrasound, endoscopy, secondary captures and screen captures often have
  the patient's name drawn into the image, where no attribute holds it. An
  image whose Burned In Annotation is YES, or that is of such a kind and does
  not say NO, is refused unless -redact says where the text is or
  -allow-burned-in says there is none. The command does not find text: the
  rectangles are yours to get right. A compressed image that is redacted
  is written uncompressed: nothing is encoded again, so no quality is lost,
  and the file is larger.

WHAT IT DOES NOT DO:
  File names are kept, so a name that identifies the patient still does.
  Files that are not DICOM are left out of the output, and a DICOMDIR is
  reported and not written.

EXAMPLES:
  go-dicom anonymize scan.dcm clean.dcm
  go-dicom anonymize study/ clean-study/
  go-dicom anonymize -profile retain-dates study/ clean-study/
  go-dicom anonymize -redact 0,0,640,40 ultrasound.dcm clean.dcm
`)
}
