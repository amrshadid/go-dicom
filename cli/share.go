package cli

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/tag"
)

// ShareCommand renders a DICOM image as a picture that can be handed on: a
// PNG or JPEG with none of the file's attributes, and not written at all
// while the image may hold text nobody has answered for.
type ShareCommand struct {
	redact        regionList
	allowBurnedIn bool
	window        string
	frame         string
	quality       int
	help          bool
}

// Name returns the command name.
func (sc *ShareCommand) Name() string {
	return "share"
}

// Description returns the command description.
func (sc *ShareCommand) Description() string {
	return "Render an image as a PNG or JPEG that is safe to hand on"
}

// AddFlags adds command flags to the flag set.
func (sc *ShareCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&sc.help, "h", false, "Show help for this command")
	fs.BoolVar(&sc.help, "help", false, "Show help for this command")
	fs.Var(&sc.redact, "redact", "Fill the rectangle x,y,width,height with black in every frame (repeatable)")
	fs.BoolVar(&sc.allowBurnedIn, "allow-burned-in", false, "Render an image that may hold burned-in text without -redact")
	fs.StringVar(&sc.window, "window", "", "Window as center,width (default: the file's)")
	fs.StringVar(&sc.frame, "frame", "", "A frame number from 1, or all (default: 1)")
	fs.IntVar(&sc.quality, "quality", 90, "JPEG quality from 1 to 100")
}

// Execute runs the share command.
func (sc *ShareCommand) Execute(args []string) error {
	flags := flag.NewFlagSet("share", flag.ContinueOnError)
	sc.AddFlags(flags)
	positional, err := ParseArgs(flags, args)
	if err != nil {
		return fmt.Errorf("share: failed to parse flags: %v", err)
	}
	if sc.help {
		sc.showHelp()
		return nil
	}
	if len(positional) != 2 {
		return fmt.Errorf("share: expected an input and an output\nRun 'go-dicom share -h' for usage")
	}
	input, output := positional[0], positional[1]

	// The extension is the format: a picture named .png that holds a JPEG is
	// a nuisance, and there is no --format here to get wrong.
	opts := imageOptions{window: sc.window, frame: sc.frame, quality: sc.quality}
	switch strings.ToLower(filepath.Ext(output)) {
	case ".png":
		opts.format = "png"
	case ".jpg", ".jpeg":
		opts.format = "jpeg"
	default:
		return fmt.Errorf("share: the output is %q; it has to end in .png, .jpg or .jpeg", output)
	}
	if err := opts.check(); err != nil {
		return fmt.Errorf("share: %v", err)
	}

	ds, err := readImageDataset(input)
	if err != nil {
		return fmt.Errorf("share: %v", err)
	}
	if _, hasPixels := ds.Get(tag.New(0x7FE0, 0x0010)); !hasPixels {
		return fmt.Errorf("share: %s has no image", input)
	}

	// The pixels are all that leaves, so they are all that is checked: the
	// same rule as anonymize, applied before anything is rendered.
	switch {
	case len(sc.redact) > 0:
		// Compressed pixel data is decoded first; nothing is written back,
		// so the input keeps its form.
		if err := ds.DecompressInPlace(); err != nil {
			return fmt.Errorf("share: %v", err)
		}
		if err := ds.RedactPixelRegions(sc.redact...); err != nil {
			return fmt.Errorf("share: %v", err)
		}
	case !sc.allowBurnedIn:
		if risk, reason := anonymize.BurnedInRisk(ds); risk {
			return fmt.Errorf("share: %s may hold text burned into the pixels: %s. "+
				"Give -redact x,y,width,height for where the text is, "+
				"or -allow-burned-in if there is none", input, reason)
		}
	}

	written, err := writeImages(ds, output, opts)
	if err != nil {
		return fmt.Errorf("share: %v", err)
	}
	for _, name := range written {
		fmt.Printf("Wrote %s\n", name)
	}
	fmt.Println("The picture holds pixels only: no name, date or identifier from the file is in it.")
	fmt.Println("Nothing looked at the picture itself. Text drawn into it is still there unless you redacted it.")
	return nil
}

func (sc *ShareCommand) showHelp() {
	fmt.Print(`Render a DICOM image as a picture that is safe to hand on

USAGE:
  go-dicom share [options] <input.dcm> <output.png|.jpg|.jpeg>

  Writes the image as a viewer shows it: rescaled, windowed and at 8 bits.
  The output's extension is its format. The picture holds pixels only, so no
  name, date or identifier from the file is in it, and the input is never
  changed.

OPTIONS:
  -redact x,y,w,h    Fill a rectangle with black in every frame, counted in
                     pixels from the top left. Repeat it for more rectangles.
  -allow-burned-in   Render an image that may hold burned-in text as it is.
  --window <c,w>     Window center and width (default: the file's own, or
                     the full range of the data when it has none).
  --frame <n|all>    The frame to write, counted from 1 (default: 1). With
                     "all", frames go to name-0001.png, name-0002.png, ...
  --quality <1-100>  JPEG quality (default: 90). PNG is lossless.
  -h, -help          Show this help message

BURNED-IN TEXT:
  Removing the attributes does nothing for a name drawn into the picture,
  which ultrasound, endoscopy, secondary captures and screen captures often
  have. An image whose Burned In Annotation is YES, or that is of such a kind
  and does not say NO, is refused unless -redact says where the text is or
  -allow-burned-in says there is none. This command does not find text: look
  at the picture before you send it.

EXAMPLES:
  go-dicom share ct.dcm ct.png --window 40,400
  go-dicom share -redact 0,0,640,40 ultrasound.dcm ultrasound.png
  go-dicom share cine.dcm frame.jpg --frame all
`)
}
