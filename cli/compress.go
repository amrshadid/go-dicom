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
	near    int
	ratio   float64
	psnr    float64
	inPlace bool
	help    bool
}

// DecompressCommand writes a file's pixel data native.
type DecompressCommand struct {
	inPlace bool
	help    bool
}

// compressParams are what a lossy target is asked with: one of them, the one
// its param names.
type compressParams struct {
	quality int
	near    int
	ratio   float64
	psnr    float64
}

// The parameter a target takes. Every lossy coder takes one, and is used only
// when it is given: what to lose is the caller's to say.
const (
	paramNone    = ""
	paramQuality = "-quality"
	paramNear    = "-near"
	paramRate    = "-ratio or -psnr"
)

// compressTarget is a syntax this library writes: an entry of the library's
// one table of coders (dataset.PixelEncoders), with the flag its parameter
// is given by.
type compressTarget struct {
	uid, name string
	param     string
	encoder   dataset.PixelEncoder
}

// paramFlags are the flags of each parameter a coder takes.
var paramFlags = map[dataset.EncoderParam]string{
	dataset.EncoderParamNone:         paramNone,
	dataset.EncoderParamJPEGQuality:  paramQuality,
	dataset.EncoderParamJPEGLSNear:   paramNear,
	dataset.EncoderParamJPEG2000Rate: paramRate,
}

// targetOf is the target of an encoder.
func targetOf(e dataset.PixelEncoder) compressTarget {
	return compressTarget{uid: e.UID, name: e.Name, param: paramFlags[e.Param], encoder: e}
}

// encoderParams are p as the data set's coders take them.
func (p compressParams) encoderParams() dataset.EncoderParams {
	return dataset.EncoderParams{JPEGQuality: p.quality, JPEGLSNear: p.near,
		JPEG2000Rate: compress.JPEG2000Rate{Ratio: p.ratio, PSNR: p.psnr}}
}

// compress codes ds's pixel data in the target's syntax.
func (t compressTarget) compress(ds *dataset.Dataset, p compressParams) error {
	return t.encoder.EncodeInPlace(ds, p.encoderParams())
}

// lossy reports whether coding with p loses anything. JPEG-LS Near-Lossless
// with an allowed error of 0 is lossless; every other target with a parameter
// is lossy at every value of it.
func (t compressTarget) lossy(p compressParams) bool {
	return t.encoder.Loses(p.encoderParams())
}

// compressNames maps what -syntax accepts to a target's UID.
var compressNames = map[string]string{
	"rle":               "1.2.840.10008.1.2.5",
	"jpeg-ls":           "1.2.840.10008.1.2.4.80",
	"jpegls":            "1.2.840.10008.1.2.4.80",
	"jls":               "1.2.840.10008.1.2.4.80",
	"jpeg-ls-near":      "1.2.840.10008.1.2.4.81",
	"jls-near":          "1.2.840.10008.1.2.4.81",
	"jpeg2000":          "1.2.840.10008.1.2.4.90",
	"jpeg-2000":         "1.2.840.10008.1.2.4.90",
	"j2k":               "1.2.840.10008.1.2.4.90",
	"jpeg2000-lossy":    "1.2.840.10008.1.2.4.91",
	"j2k-lossy":         "1.2.840.10008.1.2.4.91",
	"jpeg-lossless":     "1.2.840.10008.1.2.4.57",
	"jpeg-lossless-sv1": "1.2.840.10008.1.2.4.70",
	"jpeg":              "1.2.840.10008.1.2.4.50",
	"jpeg-baseline":     "1.2.840.10008.1.2.4.50",
	"jpeg-extended":     "1.2.840.10008.1.2.4.51",
	"jpeg-12bit":        "1.2.840.10008.1.2.4.51",
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
	if e, ok := dataset.PixelEncoderFor(uid); ok {
		return targetOf(e), nil
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

// targetNames lists the targets by their main names.
func targetNames() string {
	return "rle, jpeg-ls, jpeg-ls-near (with -near), jpeg2000, jpeg2000-lossy (with -ratio or -psnr), " +
		"jpeg-lossless, jpeg-lossless-sv1, jpeg and jpeg-extended (lossy, with -quality)"
}

// checkParams says whether the parameters given are the ones target takes:
// a lossy target's own, given and in range, and no other.
func checkParams(target compressTarget, p compressParams, nearGiven bool) error {
	given := []struct {
		param string
		set   bool
	}{
		{paramQuality, p.quality != 0},
		{paramNear, nearGiven},
		{paramRate, p.ratio != 0 || p.psnr != 0},
	}
	for _, g := range given {
		if param := g.param; g.set && param != target.param {
			if target.param == paramNone {
				return fmt.Errorf("%s is not for %s, which is lossless", param, target.name)
			}
			return fmt.Errorf("%s is not for %s, which takes %s", param, target.name, target.param)
		}
	}
	switch target.param {
	case paramQuality:
		if p.quality == 0 {
			// Lossy is never the default.
			return fmt.Errorf("%s is lossy; give -quality from 1 to 100 to accept that", target.name)
		}
		if p.quality < 1 || p.quality > 100 {
			return fmt.Errorf("-quality %d; it runs from 1 to 100", p.quality)
		}
	case paramNear:
		if !nearGiven {
			return fmt.Errorf("%s takes the error it may make on a sample; give -near, 0 for none", target.name)
		}
		if p.near < 0 || p.near > 255 {
			return fmt.Errorf("-near %d; it runs from 0 to 255", p.near)
		}
	case paramRate:
		switch {
		case p.ratio == 0 && p.psnr == 0:
			return fmt.Errorf("%s is lossy; give -ratio (a compression ratio, such as 10) or -psnr "+
				"(in decibels, such as 45) to accept that", target.name)
		case p.ratio != 0 && p.psnr != 0:
			return fmt.Errorf("give -ratio or -psnr, not both")
		case p.ratio < 0 || (p.ratio > 0 && p.ratio < 1):
			return fmt.Errorf("-ratio %g; it is 1 or more", p.ratio)
		case p.psnr < 0:
			return fmt.Errorf("-psnr %g; it is above zero", p.psnr)
		}
	}
	return nil
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
	return "Write a file's pixel data in RLE, JPEG-LS, JPEG 2000 or JPEG"
}

// AddFlags adds command flags to the flag set.
func (cc *CompressCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&cc.help, "h", false, "Show help for this command")
	fs.BoolVar(&cc.help, "help", false, "Show help for this command")
	fs.StringVar(&cc.syntax, "syntax", "", "The syntax to write: "+targetNames()+", or its UID")
	fs.IntVar(&cc.quality, "quality", 0, "For jpeg and jpeg-extended, which are lossy: quality from 1 to 100")
	fs.IntVar(&cc.near, "near", 0, "For jpeg-ls-near: the error allowed on each sample, 0 to 255 (0 is lossless)")
	fs.Float64Var(&cc.ratio, "ratio", 0, "For jpeg2000-lossy: the compression ratio to meet, such as 10")
	fs.Float64Var(&cc.psnr, "psnr", 0, "For jpeg2000-lossy: the PSNR to meet, in decibels, such as 45")
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
	// -near 0 is a value, lossless JPEG-LS Near-Lossless, so whether it was
	// given is asked of the flag set and not of the value.
	nearGiven := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "near" {
			nearGiven = true
		}
	})
	params := compressParams{quality: cc.quality, near: cc.near, ratio: cc.ratio, psnr: cc.psnr}
	if err := checkParams(target, params, nearGiven); err != nil {
		return fmt.Errorf("compress: %w", err)
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
	if err := target.compress(ds, params); err != nil {
		return fmt.Errorf("compress: %s: %s", input, withoutPrefix(err))
	}
	lossy := target.lossy(params)
	if lossy {
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
	if lossy {
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
  jpeg-ls-near (jls-near) JPEG-LS Near-Lossless, -near  1.2.840.10008.1.2.4.81
  jpeg2000 (j2k)          JPEG 2000 Lossless            1.2.840.10008.1.2.4.90
  jpeg2000-lossy          JPEG 2000, lossy, -ratio      1.2.840.10008.1.2.4.91
    (j2k-lossy)             or -psnr
  jpeg-lossless           JPEG Lossless, Process 14     1.2.840.10008.1.2.4.57
  jpeg-lossless-sv1       JPEG Lossless, predictor 1    1.2.840.10008.1.2.4.70
  jpeg (jpeg-baseline)    JPEG Baseline, lossy,         1.2.840.10008.1.2.4.50
                            -quality
  jpeg-extended           JPEG Extended (12-bit),       1.2.840.10008.1.2.4.51
    (jpeg-12bit)            lossy, -quality

  The lossless ones return every sample as it was. Compressed pixel data is
  decoded first, so a file moves from one syntax to another. A lossy one is
  used only when its parameter is given: what to lose is never chosen for you.

OPTIONS:
  -syntax <name|uid>   The syntax to write (required).
  -quality <1-100>     For jpeg and jpeg-extended, and required with them. They
                       lose detail at every quality.
  -near <0-255>        For jpeg-ls-near, and required with it: every sample
                       comes back within this much of its value. 0 is lossless.
  -ratio <r>           For jpeg2000-lossy: the compression ratio to meet, the
                       codestream no larger than the samples over r.
  -psnr <dB>           For jpeg2000-lossy, instead of -ratio: the PSNR the
                       decoded image is to have at least.
  -in-place            Write over the input, through a new file renamed over it.
  -h, -help            Show this help message

WHAT CHANGES:
  The transfer syntax and Pixel Data. A color image says Planar Configuration
  0; an RGB image in JPEG 2000 Lossless is YBR_RCT, in lossy JPEG 2000 YBR_ICT,
  and in JPEG Baseline and Extended YBR_FULL; lossless JPEG and JPEG-LS keep
  RGB. JPEG Baseline takes 8-bit unsigned grayscale and color; JPEG Extended
  takes those and unsigned grayscale of up to 12 bits stored; JPEG-LS with
  -near above 0 takes unsigned images. Lossy JPEG 2000 takes signed images too.
  Lossless: the SOP Instance UID is kept, and so is any record of a loss
  made before.
  Lossy: Lossy Image Compression becomes 01, its ratio and method
  (ISO_10918_1, ISO_14495_1 or ISO_15444_1) are added to any recorded, and the
  instance is given a new SOP Instance UID, since a lossy copy is a different
  instance (PS3.3 C.7.6.1.1.5).

EXAMPLES:
  go-dicom compress -syntax rle ct.dcm ct-rle.dcm
  go-dicom compress -syntax jpeg-ls -in-place mr.dcm
  go-dicom compress -syntax jpeg-lossless-sv1 ct.dcm ct-jpll.dcm
  go-dicom compress -syntax 1.2.840.10008.1.2.4.90 us.dcm us-j2k.dcm
  go-dicom compress -syntax jpeg -quality 90 sc.dcm sc-jpeg.dcm
  go-dicom compress -syntax jpeg-extended -quality 90 cr.dcm cr-jpeg12.dcm
  go-dicom compress -syntax jpeg-ls-near -near 2 us.dcm us-jls.dcm
  go-dicom compress -syntax jpeg2000-lossy -ratio 10 mr.dcm mr-j2k.dcm
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
