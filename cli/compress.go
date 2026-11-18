package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/valuerep"
)

// CompressCommand writes a file's pixel data in a compressed transfer syntax.
type CompressCommand struct {
	syntax  string
	quality int
	inPlace bool
	help    bool
}

// DecompressCommand writes a file's pixel data native.
type DecompressCommand struct {
	inPlace bool
	help    bool
}

// compressTarget is a syntax this library writes.
type compressTarget struct {
	uid, name string
	lossy     bool
	compress  func(ds *dataset.Dataset, quality int) error
}

// compressTargets are the four syntaxes the data set's coders write, under the
// names the command takes. JPEG Baseline is the one lossy coder, and is
// named and given a quality or not used.
var compressTargets = []compressTarget{
	{"1.2.840.10008.1.2.5", "RLE Lossless", false,
		func(ds *dataset.Dataset, _ int) error { return ds.CompressRLELosslessInPlace() }},
	{"1.2.840.10008.1.2.4.80", "JPEG-LS Lossless", false,
		func(ds *dataset.Dataset, _ int) error { return ds.CompressJPEGLSLosslessInPlace() }},
	{"1.2.840.10008.1.2.4.90", "JPEG 2000 Lossless", false,
		func(ds *dataset.Dataset, _ int) error { return ds.CompressJPEG2000LosslessInPlace() }},
	{"1.2.840.10008.1.2.4.50", "JPEG Baseline", true,
		func(ds *dataset.Dataset, quality int) error { return ds.CompressJPEGBaselineInPlace(quality) }},
}

// compressNames maps what -syntax accepts to a target's UID.
var compressNames = map[string]string{
	"rle":           "1.2.840.10008.1.2.5",
	"jpeg-ls":       "1.2.840.10008.1.2.4.80",
	"jpegls":        "1.2.840.10008.1.2.4.80",
	"jls":           "1.2.840.10008.1.2.4.80",
	"jpeg2000":      "1.2.840.10008.1.2.4.90",
	"jpeg-2000":     "1.2.840.10008.1.2.4.90",
	"j2k":           "1.2.840.10008.1.2.4.90",
	"jpeg":          "1.2.840.10008.1.2.4.50",
	"jpeg-baseline": "1.2.840.10008.1.2.4.50",
}

// notWritten names the compressed syntaxes people ask for that no coder here
// writes, so the refusal says which one it is rather than that the name is
// unknown.
var notWritten = map[string]string{
	"1.2.840.10008.1.2.4.91": "JPEG 2000 (lossy)",
	"1.2.840.10008.1.2.4.57": "JPEG Lossless (Process 14)",
	"1.2.840.10008.1.2.4.70": "JPEG Lossless (Process 14, first-order prediction)",
	"1.2.840.10008.1.2.4.51": "JPEG Extended (12-bit, Process 2 and 4)",
	"1.2.840.10008.1.2.4.81": "JPEG-LS Near-Lossless",
}

var notWrittenNames = map[string]string{
	"jpeg2000-lossy": "1.2.840.10008.1.2.4.91",
	"j2k-lossy":      "1.2.840.10008.1.2.4.91",
	"jpeg-lossless":  "1.2.840.10008.1.2.4.70",
	"jpeg-extended":  "1.2.840.10008.1.2.4.51",
	"jpeg-12bit":     "1.2.840.10008.1.2.4.51",
	"jpeg-ls-near":   "1.2.840.10008.1.2.4.81",
	"jls-near":       "1.2.840.10008.1.2.4.81",
}

// findTarget reads -syntax: a name or a UID.
func findTarget(given string) (compressTarget, error) {
	key := strings.ToLower(strings.TrimSpace(given))
	if key == "" {
		return compressTarget{}, fmt.Errorf("give -syntax: %s", targetNames())
	}
	uid := key
	if named, ok := compressNames[key]; ok {
		uid = named
	}
	if named, ok := notWrittenNames[key]; ok {
		uid = named
	}
	for _, t := range compressTargets {
		if t.uid == uid {
			return t, nil
		}
	}
	if name, ok := notWritten[uid]; ok {
		return compressTarget{}, fmt.Errorf("%s (%s) is not written by this tool; it writes %s", name, uid, targetNames())
	}
	if valuerep.ValidateUID(uid) != nil {
		return compressTarget{}, fmt.Errorf("%q is neither a syntax name nor a transfer syntax UID; the names are %s", given, targetNames())
	}
	if compress.IsEncapsulated(uid) {
		return compressTarget{}, fmt.Errorf("%s is a compressed syntax this tool does not write; it writes %s", uid, targetNames())
	}
	if strings.HasPrefix(uid, "1.2.840.10008.1.2") {
		return compressTarget{}, fmt.Errorf("%s is not a compressed syntax; decompress writes native pixel data", uid)
	}
	return compressTarget{}, fmt.Errorf("%q is neither a syntax name nor a transfer syntax UID; the names are %s", given, targetNames())
}

// targetNames lists the four targets by their main names.
func targetNames() string {
	return "rle, jpeg-ls, jpeg2000, and jpeg (lossy, with -quality)"
}

// withoutPrefix drops the "compress: " a data set's coder starts its error
// with, which the command's own prefix already says.
func withoutPrefix(err error) string {
	return strings.TrimPrefix(strings.TrimPrefix(err.Error(), "compress: "), "decompress: ")
}

// Name returns the command name.
func (cc *CompressCommand) Name() string { return "compress" }

// Description returns the command description.
func (cc *CompressCommand) Description() string {
	return "Write a file's pixel data in RLE, JPEG-LS, JPEG 2000 or JPEG Baseline"
}

// AddFlags adds command flags to the flag set.
func (cc *CompressCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&cc.help, "h", false, "Show help for this command")
	fs.BoolVar(&cc.help, "help", false, "Show help for this command")
	fs.StringVar(&cc.syntax, "syntax", "", "The syntax to write: "+targetNames()+", or its UID")
	fs.IntVar(&cc.quality, "quality", 0, "For jpeg, which is lossy: quality from 1 to 100")
	fs.BoolVar(&cc.inPlace, "in-place", false, "Write over the input instead of to a new file")
}

// Execute runs the compress command.
func (cc *CompressCommand) Execute(args []string) error {
	flags := flag.NewFlagSet("compress", flag.ContinueOnError)
	cc.AddFlags(flags)
	flags.SetOutput(io.Discard)
	positional, err := ParseArgs(flags, args)
	if err != nil {
		return fmt.Errorf("compress: %v\nRun 'go-dicom compress -h' for usage", err)
	}
	if cc.help {
		cc.showHelp()
		return nil
	}
	target, err := findTarget(cc.syntax)
	if err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	switch {
	case target.lossy && cc.quality == 0:
		// Lossy is never the default: what to lose is the caller's to say.
		return fmt.Errorf("compress: %s is lossy; give -quality from 1 to 100 to accept that", target.name)
	case target.lossy && (cc.quality < 1 || cc.quality > 100):
		return fmt.Errorf("compress: -quality %d; it runs from 1 to 100", cc.quality)
	case !target.lossy && cc.quality != 0:
		return fmt.Errorf("compress: -quality is for jpeg; %s is lossless", target.name)
	}
	input, output, err := outputPaths("compress", positional, cc.inPlace)
	if err != nil {
		return err
	}
	df, ds, err := readForRewrite("compress", input)
	if err != nil {
		return err
	}
	if _, ok := ds.Get(tag.New(0x7FE0, 0x0010)); !ok {
		return fmt.Errorf("compress: %s has no Pixel Data to compress", input)
	}

	meta := instanceMeta(df, ds)
	if meta.TransferSyntaxUID == target.uid {
		return fmt.Errorf("compress: %s is %s already", input, target.name)
	}
	if err := target.compress(ds, cc.quality); err != nil {
		return fmt.Errorf("compress: %s: %s", input, withoutPrefix(err))
	}
	if target.lossy {
		// PS3.3 C.7.6.1.1.5: a lossily compressed image is a new instance.
		// The coder records the loss; the identity is the command's to give,
		// and the meta, made below from the data set, follows it.
		if _, err := newInstanceUID(ds); err != nil {
			return fmt.Errorf("compress: %w", err)
		}
		meta = instanceMeta(df, ds)
	}
	meta.TransferSyntaxUID = ds.TransferSyntaxUID()
	if err := writeOutput(output, cc.inPlace, meta, ds); err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	fmt.Printf("Wrote %s: %s (%s)\n", output, target.name, target.uid)
	if target.lossy {
		fmt.Printf("  lossy: Lossy Image Compression is 01, and the instance has a new SOP Instance UID, %s\n",
			meta.MediaStorageSOPInstanceUID)
	}
	return nil
}

func (cc *CompressCommand) showHelp() {
	printHelp(`Write a file's pixel data in a compressed transfer syntax

USAGE:
  go-dicom compress -syntax <name|uid> [options] <input> <output>
  go-dicom compress -syntax <name|uid> [options] -in-place <file>

SYNTAXES:
  rle                     RLE Lossless                  1.2.840.10008.1.2.5
  jpeg-ls (jpegls, jls)   JPEG-LS Lossless              1.2.840.10008.1.2.4.80
  jpeg2000 (j2k)          JPEG 2000 Lossless            1.2.840.10008.1.2.4.90
  jpeg (jpeg-baseline)    JPEG Baseline, lossy          1.2.840.10008.1.2.4.50

  The three lossless ones return every sample as it was. Compressed pixel
  data is decoded first, so a file moves from one syntax to another.

  Not written, and refused by name: lossy JPEG 2000 (.91), JPEG Lossless
  (.57, .70), 12-bit JPEG Extended (.51) and JPEG-LS Near-Lossless (.81).

OPTIONS:
  -syntax <name|uid>   The syntax to write (required).
  -quality <1-100>     For jpeg, and required with it: JPEG Baseline loses
                       detail at every quality, so it is never chosen for you.
  -in-place            Write over the input, through a new file renamed over it.
  -h, -help            Show this help message

WHAT CHANGES:
  The transfer syntax and Pixel Data. A color image says Planar Configuration
  0; an RGB image in JPEG 2000 is YBR_RCT, and in JPEG Baseline YBR_FULL.
  JPEG Baseline takes 8-bit unsigned grayscale, RGB and YBR_FULL images.
  Lossless: the SOP Instance UID is kept, and so is any record of a loss
  made before.
  Lossy (jpeg): Lossy Image Compression becomes 01, its ratio and method
  ISO_10918_1 are added to any recorded, and the instance is given a new
  SOP Instance UID, since a lossy copy is a different instance (PS3.3
  C.7.6.1.1.5).

EXAMPLES:
  go-dicom compress -syntax rle ct.dcm ct-rle.dcm
  go-dicom compress -syntax jpeg-ls -in-place mr.dcm
  go-dicom compress -syntax 1.2.840.10008.1.2.4.90 us.dcm us-j2k.dcm
  go-dicom compress -syntax jpeg -quality 90 sc.dcm sc-jpeg.dcm
`)
}

// Name returns the command name.
func (dc *DecompressCommand) Name() string { return "decompress" }

// Description returns the command description.
func (dc *DecompressCommand) Description() string {
	return "Write a file's pixel data uncompressed"
}

// AddFlags adds command flags to the flag set.
func (dc *DecompressCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&dc.help, "h", false, "Show help for this command")
	fs.BoolVar(&dc.help, "help", false, "Show help for this command")
	fs.BoolVar(&dc.inPlace, "in-place", false, "Write over the input instead of to a new file")
}

// Execute runs the decompress command.
func (dc *DecompressCommand) Execute(args []string) error {
	flags := flag.NewFlagSet("decompress", flag.ContinueOnError)
	dc.AddFlags(flags)
	flags.SetOutput(io.Discard)
	positional, err := ParseArgs(flags, args)
	if err != nil {
		return fmt.Errorf("decompress: %v\nRun 'go-dicom decompress -h' for usage", err)
	}
	if dc.help {
		dc.showHelp()
		return nil
	}
	input, output, err := outputPaths("decompress", positional, dc.inPlace)
	if err != nil {
		return err
	}
	df, ds, err := readForRewrite("decompress", input)
	if err != nil {
		return err
	}
	meta := instanceMeta(df, ds)
	if !compress.IsEncapsulated(meta.TransferSyntaxUID) {
		return fmt.Errorf("decompress: %s is not compressed: its transfer syntax is %s", input, meta.TransferSyntaxUID)
	}
	if err := ds.DecompressInPlace(); err != nil {
		return fmt.Errorf("decompress: %s: %s", input, withoutPrefix(err))
	}
	if _, ok := ds.Get(tag.New(0x7FE0, 0x0010)); ok {
		meta.TransferSyntaxUID = ds.TransferSyntaxUID()
	} else {
		// No pixel data: nothing to decode, and the data set is written in
		// the native syntax all the same.
		meta.TransferSyntaxUID = "1.2.840.10008.1.2.1"
	}
	if err := writeOutput(output, dc.inPlace, meta, ds); err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	fmt.Printf("Wrote %s: Explicit VR Little Endian (%s)\n", output, meta.TransferSyntaxUID)
	return nil
}

func (dc *DecompressCommand) showHelp() {
	printHelp(`Write a file's pixel data uncompressed

USAGE:
  go-dicom decompress [options] <input> <output>
  go-dicom decompress [options] -in-place <file>

  Decodes the pixel data of a compressed file, in any syntax this tool
  reads (RLE, JPEG Baseline, Extended and Lossless, JPEG-LS, JPEG 2000), and
  writes it as Explicit VR Little Endian.

OPTIONS:
  -in-place   Write over the input, through a new file renamed over it.
  -h, -help   Show this help message

WHAT CHANGES:
  The transfer syntax and Pixel Data, and what describes the samples as
  they come out: Planar Configuration 0 for color, YBR_FULL for a 4:2:2
  JPEG, RGB for a JPEG 2000 color transform. The SOP Instance UID is kept,
  and so is Lossy Image Compression: decoding does not undo a loss.

EXAMPLES:
  go-dicom decompress ct-jpeg.dcm ct.dcm
  go-dicom decompress -in-place us.dcm
`)
}
