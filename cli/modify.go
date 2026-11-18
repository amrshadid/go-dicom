package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// ModifyCommand sets, inserts and deletes attributes of a DICOM file.
type ModifyCommand struct {
	edits         editList
	newUID        bool
	inPlace       bool
	ignoreMissing bool
	help          bool
}

// editList collects -set, -insert and -delete in the order they were given:
// the three flags share it, so an insert followed by a delete of the same
// attribute does what it says.
type editList struct {
	edits []edit
}

// editFlag is one of the three flags, appending to the shared list.
type editFlag struct {
	list *editList
	kind editKind
}

func (f editFlag) String() string { return "" }

func (f editFlag) Set(value string) error {
	e, err := parseEdit(f.kind, value)
	if err != nil {
		return err
	}
	f.list.edits = append(f.list.edits, e)
	return nil
}

// Name returns the command name.
func (mc *ModifyCommand) Name() string {
	return "modify"
}

// Description returns the command description.
func (mc *ModifyCommand) Description() string {
	return "Set, insert or delete attributes of a DICOM file"
}

// AddFlags adds command flags to the flag set.
func (mc *ModifyCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&mc.help, "h", false, "Show help for this command")
	fs.BoolVar(&mc.help, "help", false, "Show help for this command")
	fs.Var(editFlag{&mc.edits, editSet}, "set", "Change an attribute that is there: path=value (repeatable)")
	fs.Var(editFlag{&mc.edits, editInsert}, "insert", "Set an attribute, creating it and the items on its path: path=value (repeatable)")
	fs.Var(editFlag{&mc.edits, editDelete}, "delete", "Remove an attribute, or an item when the path ends in one (repeatable)")
	fs.BoolVar(&mc.newUID, "new-uid", false, "Give the instance a new SOP Instance UID")
	fs.BoolVar(&mc.inPlace, "in-place", false, "Write over the input instead of to a new file")
	fs.BoolVar(&mc.ignoreMissing, "ignore-missing", false, "Skip a -set or -delete whose attribute is not in the file")
}

// Execute runs the modify command.
func (mc *ModifyCommand) Execute(args []string) error {
	flags := flag.NewFlagSet("modify", flag.ContinueOnError)
	mc.AddFlags(flags)
	// A refused edit is reported once, in the error, not under a usage
	// listing that buries it.
	flags.SetOutput(io.Discard)
	positional, err := ParseArgs(flags, args)
	if err != nil {
		return fmt.Errorf("modify: %v\nRun 'go-dicom modify -h' for usage", err)
	}
	if mc.help {
		mc.showHelp()
		return nil
	}

	input, output, err := outputPaths("modify", positional, mc.inPlace)
	if err != nil {
		return err
	}
	if len(mc.edits.edits) == 0 && !mc.newUID {
		return fmt.Errorf("modify: nothing to do; give -set, -insert, -delete or -new-uid")
	}
	df, ds, err := readForRewrite("modify", input)
	if err != nil {
		return err
	}

	// Everything is applied before anything is written, so a refused edit
	// leaves no file behind and the input untouched.
	changed := 0
	for _, e := range mc.edits.edits {
		n, err := e.apply(ds)
		if errors.Is(err, errMissing) && mc.ignoreMissing {
			fmt.Fprintf(os.Stderr, "skipped %s %s: %v\n", e.kind.flag(), e.given, err)
			continue
		}
		if err != nil {
			return fmt.Errorf("modify: %s %s: %w", e.kind.flag(), e.given, err)
		}
		changed += n
	}
	if mc.newUID {
		if _, err := newInstanceUID(ds); err != nil {
			return fmt.Errorf("modify: %w", err)
		}
		changed++
	}

	meta := instanceMeta(df, ds)
	if err := writeOutput(output, mc.inPlace, meta, ds); err != nil {
		return fmt.Errorf("modify: %w", err)
	}
	fmt.Printf("Wrote %s: %d change(s), transfer syntax %s\n", output, changed, meta.TransferSyntaxUID)
	return nil
}

func (mc *ModifyCommand) showHelp() {
	printHelp(`Set, insert or delete attributes of a DICOM file

USAGE:
  go-dicom modify [options] <input> <output>
  go-dicom modify [options] -in-place <file>

  The input is read whole, every edit is made in the order given, and the
  result is written to <output>. Nothing is written if any edit is refused.
  The input is changed only with -in-place, and then through a new file
  renamed over it, so it is never left half written.

OPTIONS:
  -set <path>=<value>     Change an attribute that is in the file. It is an
                          error if it is not (dcmodify -m).
  -insert <path>=<value>  Set an attribute whether it is there or not,
                          creating the sequences and items on its path
                          (dcmodify -i).
  -delete <path>          Remove an attribute, or an item when the path ends
                          in [n], or every item with [*] (dcmodify -e).
  -new-uid                Give the instance a new SOP Instance UID. Without
                          it the instance keeps its UID, whatever changed.
  -in-place               Write over the input.
  -ignore-missing         Skip a -set or -delete whose attribute is not in
                          the file, saying so, instead of failing.
  -h, -help               Show this help message

  -set, -insert and -delete may each be given many times.

PATHS:
  An attribute is a keyword or a tag:
    PatientName   (0010,0010)   0010,0010   00100010
  Inside a sequence, the item is named in brackets, counted from 0, or [*]
  for every item, and the attribute in it follows a dot, to any depth:
    ReferencedSeriesSequence[0].SeriesInstanceUID
    (0008,1115)[*].(0008,114A)[0].(0008,1155)
  A private attribute is named by its group, its element within the block
  as xxee, and its private creator in double quotes; a VR may follow when
  the private dictionary does not know it:
    (0029,xx10,"ACME 1.0")        (0029,xx10,"ACME 1.0",LO)
  Its block (the xx) is looked up in the file. An insert reserves one when
  the creator has none, as PS3.5 7.8.1 requires. A private tag by number,
  (0029,1010), may be set or deleted, and inserted only when the creator of
  its block is there.

VALUES:
  Multiple values are separated by a backslash: -set 'ImageType=DERIVED\SECONDARY'.
  An empty value (Tag=) writes the attribute with no value.
  Each value is checked against the attribute's VR before anything is
  written: dates (DA) as YYYYMMDD, UIDs, code strings, the range of US, SS,
  UL and SL, the number of values the dictionary allows, and the lengths
  PS3.5 6.2 gives. Text outside ASCII needs a Specific Character Set: in a
  file that declares none, insert SpecificCharacterSet='ISO_IR 192' first.
  Binary values (OB, OW, OF, OD, OL, OV, UN) and Pixel Data are not set
  here. The file meta group (0002) is written from the data set and the
  transfer syntax, so it is not edited directly: change SOPInstanceUID, or
  give -new-uid, and the meta follows.

WHAT IS KEPT:
  The transfer syntax. Pixel data is copied as it was read, compressed or
  not, and nothing is decoded or encoded again. Text is read into UTF-8,
  so a file that declared another character set is written declaring
  ISO_IR 192, with the same characters, as anonymize writes it; for the
  same reason SpecificCharacterSet may only be set to ISO_IR 192.

EXAMPLES:
  go-dicom modify -set PatientName='Doe^Jane' in.dcm out.dcm
  go-dicom modify -insert AccessionNumber=A1234 -delete PatientBirthDate in.dcm out.dcm
  go-dicom modify -insert 'RequestAttributesSequence[0].RequestedProcedureID=RP1' in.dcm out.dcm
  go-dicom modify -delete 'ReferencedImageSequence[1]' in.dcm out.dcm
  go-dicom modify -insert '(0029,xx10,"ACME 1.0",LO)=v2' in.dcm out.dcm
  go-dicom modify -new-uid -set SeriesDescription=Corrected -in-place scan.dcm
`)
}
