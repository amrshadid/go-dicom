package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/tag"
)

// ConvertCommand converts DICOM files to other formats.
type ConvertCommand struct {
	inputFile  string
	outputFile string
	format     string
	compressed bool
	help       bool

	// For --format png and jpeg.
	window  string
	frame   string
	quality int
}

// NewConvertCommand creates a new convert command.
func NewConvertCommand() *ConvertCommand {
	return &ConvertCommand{
		format: "json",
	}
}

// Name returns the command name.
func (cc *ConvertCommand) Name() string {
	return "convert"
}

// Description returns the command description.
func (cc *ConvertCommand) Description() string {
	return "Convert DICOM files to other formats (json, csv, nifti, png, jpeg)"
}

// AddFlags adds command flags to the flag set.
func (cc *ConvertCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&cc.help, "h", false, "Show help for this command")
	fs.BoolVar(&cc.help, "help", false, "Show help for this command")
	fs.StringVar(&cc.format, "format", "json", "Output format: json, dicom-json, csv, nifti, png, jpeg")
	fs.BoolVar(&cc.compressed, "compress", false, "Apply compression to output")
	fs.StringVar(&cc.window, "window", "", "For png and jpeg: window as center,width (default: the file's)")
	fs.StringVar(&cc.frame, "frame", "", "For png and jpeg: a frame number from 1, or all (default: 1)")
	fs.IntVar(&cc.quality, "quality", 90, "For jpeg: quality from 1 to 100")
}

// Execute runs the convert command.
func (cc *ConvertCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	cc.AddFlags(fs)

	positional, err := ParseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("convert: failed to parse flags: %v", err)
	}

	// Check if help flag was set
	if cc.help {
		cc.showHelp()
		return nil
	}

	remainingArgs := positional
	if len(remainingArgs) < 2 {
		return fmt.Errorf("convert: missing arguments (input output)\nRun 'go-dicom convert -h' for usage")
	}

	cc.inputFile = remainingArgs[0]
	cc.outputFile = remainingArgs[1]

	if _, err := os.Stat(cc.inputFile); os.IsNotExist(err) {
		return fmt.Errorf("convert: input file not found: %s", cc.inputFile)
	}

	validFormats := map[string]bool{
		"json":  true,
		"csv":   true,
		"nifti": true,
		"png":   true,
		"jpeg":  true,

		"dicom-json": true,
	}
	if !validFormats[cc.format] {
		return fmt.Errorf("convert: unsupported format: %s", cc.format)
	}

	// DICOM JSON is made from the data set, sequences and binary values
	// included, which the element list behind "json" leaves out.
	if cc.format == "dicom-json" {
		if cc.compressed {
			return fmt.Errorf("convert: --compress does not apply to dicom-json")
		}
		return convertToDICOMJSON(cc.inputFile, cc.outputFile)
	}

	// An image is rendered from the data set, not from the element list the
	// other formats are built on, and may be several files.
	if cc.format == "png" || cc.format == "jpeg" {
		if cc.compressed {
			return fmt.Errorf("convert: --compress does not apply to %s, which is compressed already", cc.format)
		}
		written, err := convertToImages(cc.inputFile, cc.outputFile, imageOptions{
			format: cc.format, window: cc.window, frame: cc.frame, quality: cc.quality,
		})
		if err != nil {
			return fmt.Errorf("convert: %v", err)
		}
		for _, name := range written {
			fmt.Printf("Wrote %s\n", name)
		}
		return nil
	}

	fmt.Printf("Reading DICOM file: %s\n", cc.inputFile)
	elements, err := readDICOMFile(cc.inputFile)
	if err != nil {
		return fmt.Errorf("convert: failed to read DICOM file: %v", err)
	}

	fmt.Printf("Converting to %s format (%d elements)...\n", cc.format, len(elements))

	var data []byte
	switch cc.format {
	case "json":
		data, err = convertToJSON(elements)
	case "csv":
		data, err = convertToCSV(elements)
	case "nifti":
		data, err = convertToNifti(cc.inputFile)
	}

	if err != nil {
		return fmt.Errorf("convert: conversion failed: %v", err)
	}

	if cc.compressed {
		fmt.Println("Applying DEFLATE compression...")
		compressor := &compress.DeflateCompressor{}
		originalSize := len(data)
		compressed, err := compressor.Compress(data)
		if err != nil {
			return fmt.Errorf("convert: compression failed: %v", err)
		}
		data = compressed
		fmt.Printf("Compression ratio: %.1f%% (%d -> %d bytes)\n",
			float64(len(data))/float64(originalSize)*100, originalSize, len(data))
	}

	fmt.Printf("Writing output: %s\n", cc.outputFile)
	if err := os.WriteFile(cc.outputFile, data, 0644); err != nil {
		return fmt.Errorf("convert: failed to write output file: %v", err)
	}

	fmt.Printf("Output size: %d bytes\n", len(data))
	if cc.compressed {
		fmt.Println("Conversion completed successfully (compressed)!")
	} else {
		fmt.Println("Conversion completed successfully!")
	}

	return nil
}

// convertToDICOMJSON writes the data set as the DICOM JSON Model (PS3.18
// Annex F), binary values inline, which `create` reads back.
//
// The model has no file meta, so it cannot say what transfer syntax the
// pixel data is in. Native pixel data needs nothing more; encapsulated
// fragments are written as they are and the syntax to give `create` is
// printed, since decoding them here would make a different file.
func convertToDICOMJSON(input, output string) error {
	df, ds, err := readForRewrite("convert", input)
	if err != nil {
		return err
	}
	m, err := ds.ToDICOMJSON()
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	if err := os.WriteFile(output, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	fmt.Printf("Wrote %s: DICOM JSON, %d top-level elements\n", output, len(m))
	if syntax := instanceMeta(df, ds).TransferSyntaxUID; compress.IsEncapsulated(syntax) && ds.Contains(tag.New(0x7FE0, 0x0010)) {
		fmt.Printf("  its Pixel Data is compressed, in %s; create it back with -syntax %s\n", syntax, syntax)
	}
	return nil
}

// showHelp displays help information for the convert command.
func (cc *ConvertCommand) showHelp() {
	printHelp(`COMMAND: convert - Convert DICOM to other formats

USAGE:
  go-dicom convert <input> <output> [options]

DESCRIPTION:
  Convert a DICOM file to another format for integration with other tools
  or for data analysis.

OPTIONS:
  -h, --help          Show help for this command
  --format <format>   Output format: json, dicom-json, csv, nifti, png, jpeg
                      (default: json)
  --compress          Apply compression to output (json, csv, nifti)

DICOM JSON (--format dicom-json):
  The DICOM JSON Model of PS3.18 Annex F, as DICOMweb serves it: every
  element, sequences and binary values (as InlineBinary) included, which
  "go-dicom create" reads back into a file. "json" is a flat rendering for
  reading, without sequences or binary values, and cannot be read back.

IMAGES (--format png or jpeg):
  --window <c,w>      Window center and width, in the image's output units
                      (Hounsfield units for CT). Default: the file's own
                      window, or the full range of the data when it has none.
  --frame <n|all>     The frame to write, counted from 1 (default: 1). With
                      "all", each frame is written beside the output name as
                      name-0001.png, name-0002.png and so on.
  --quality <1-100>   JPEG quality (default: 90). PNG is lossless.

  The image is what a viewer shows: rescaled, windowed, MONOCHROME1 inverted,
  and color converted to RGB, at 8 bits. It holds pixels only, so none of the
  file's attributes are in it. Text burned into the picture itself still is.

EXAMPLES:
  go-dicom convert patient.dcm output.json
  go-dicom convert patient.dcm output.json --format json
  go-dicom convert patient.dcm dicom.json --format dicom-json
  go-dicom convert patient.dcm data.csv --format csv
  go-dicom convert patient.dcm image.nii --format nifti
  go-dicom convert ct.dcm ct.png --format png --window 40,400
  go-dicom convert cine.dcm frame.jpg --format jpeg --frame all
  go-dicom convert patient.dcm output.json --compress
  go-dicom convert -h
`)
}
