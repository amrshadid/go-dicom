package cli

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/tag"
)

// TagDocCommand generates documentation for DICOM tags.
type TagDocCommand struct {
	keyword   string
	format    string
	retired   bool
	privateOK bool
	output    string
	limit     int
	help      bool
}

// NewTagDocCommand creates a new tag-doc command.
func NewTagDocCommand() *TagDocCommand {
	return &TagDocCommand{}
}

// Name returns the command name.
func (tc *TagDocCommand) Name() string {
	return "tag-doc"
}

// Description returns the command description.
func (tc *TagDocCommand) Description() string {
	return "Generate documentation for DICOM tags from the dictionary"
}

// AddFlags adds command flags to the flag set.
func (tc *TagDocCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&tc.help, "h", false, "Show help for this command")
	fs.BoolVar(&tc.help, "help", false, "Show help for this command")
	fs.StringVar(&tc.keyword, "keyword", "", "Tag keyword to document (e.g., PatientName)")
	fs.StringVar(&tc.format, "format", "text", "Output format: text, markdown, or json")
	fs.BoolVar(&tc.retired, "retired", false, "Include retired tags in search")
	fs.BoolVar(&tc.privateOK, "private", false, "Include private tags in search")
	fs.StringVar(&tc.output, "output", "", "Write the listing to a file instead of stdout")
	fs.IntVar(&tc.limit, "limit", 50, "How many tags to list; 0 for all")
}

// Execute runs the tag-doc command.
func (tc *TagDocCommand) Execute(args []string) error {
	fs := flag.NewFlagSet(tc.Name(), flag.ContinueOnError)
	tc.AddFlags(fs)

	positional, err := ParseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("tag-doc: failed to parse flags: %v", err)
	}

	// Check if help flag was set
	if tc.help {
		tc.showHelp()
		return nil
	}

	if tc.keyword != "" {
		dict := tag.GlobalDictionary()
		t := dict.GetByKeyword(tc.keyword)
		if t == 0 {
			return fmt.Errorf("tag-doc: keyword not found: %s", tc.keyword)
		}

		info := t.GetInfo()
		if info == nil {
			return fmt.Errorf("tag-doc: tag info not available for %s", t.String())
		}

		// Display single tag documentation
		return tc.displayTagDoc(t, info)
	}

	// A tag given as an argument, which is how the help and the README show it:
	//
	//	go-dicom tag-doc 0010,0010
	//
	// Positional arguments used to be read and discarded, so that command printed
	// the first fifty tags in the dictionary and exited zero — an answer to a
	// question nobody asked, and indistinguishable from success.
	if len(positional) > 0 {
		t, err := parseTagArgument(positional[0])
		if err != nil {
			return fmt.Errorf("tag-doc: %w", err)
		}

		info := t.GetInfo()
		if info == nil {
			return fmt.Errorf("tag-doc: %s is not in the dictionary", t.String())
		}
		return tc.displayTagDoc(t, info)
	}

	// Generate documentation for all tags or by pattern
	return tc.generateTagDocumentation()
}

// displayTagDoc displays documentation for a single tag
func (tc *TagDocCommand) displayTagDoc(t tag.Tag, info *tag.TagInfo) error {
	switch tc.format {
	case "markdown":
		return tc.displayMarkdown(t, info)
	case "json":
		return tc.displayJSON(t, info)
	default: // text
		return tc.displayText(t, info)
	}
}

// displayText displays tag info in plain text format
func (tc *TagDocCommand) displayText(t tag.Tag, info *tag.TagInfo) error {
	fmt.Printf("DICOM Tag Documentation\n")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("\nTag:      %s (%s)\n", t.String(), t.Hex())
	fmt.Printf("Name:     %s\n", info.Name)
	fmt.Printf("Keyword:  %s\n", info.Keyword)
	fmt.Printf("VR:       %s\n", info.VR)
	fmt.Printf("VM:       %s\n", info.VM)

	if info.Retired {
		fmt.Printf("Status:   RETIRED\n")
	} else {
		fmt.Printf("Status:   ACTIVE\n")
	}

	if t.IsPrivate() {
		fmt.Printf("Type:     PRIVATE\n")
	} else {
		fmt.Printf("Type:     STANDARD\n")
	}

	fmt.Println("\nDescription:")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("VR: %s - %s\n", info.VR, vrDescription(info.VR))
	fmt.Printf("VM: %s - Value Multiplicity\n", info.VM)

	if info.Retired {
		fmt.Println("\n⚠️  WARNING: This tag is RETIRED and should not be used in new applications.")
	}

	return nil
}

// displayMarkdown displays tag info in Markdown format
func (tc *TagDocCommand) displayMarkdown(t tag.Tag, info *tag.TagInfo) error {
	fmt.Printf("# %s\n\n", info.Name)
	fmt.Printf("**Tag:** `%s` (`%s`)\n\n", t.String(), t.Hex())
	fmt.Printf("**Keyword:** `%s`\n\n", info.Keyword)
	fmt.Printf("| Attribute | Value |\n")
	fmt.Printf("|-----------|-------|\n")
	fmt.Printf("| VR        | %s    |\n", info.VR)
	fmt.Printf("| VM        | %s    |\n", info.VM)
	fmt.Printf("| Type      | %s    |\n", tagType(t))
	fmt.Printf("| Status    | %s    |\n", tagStatus(info))
	fmt.Println()

	if info.Retired {
		fmt.Println("⚠️ **WARNING:** This tag is retired and should not be used.")
	}

	fmt.Printf("## Details\n\n")
	fmt.Printf("- **VR:** %s - %s\n", info.VR, vrDescription(info.VR))
	fmt.Printf("- **VM:** %s - Value Multiplicity\n", info.VM)

	return nil
}

// displayJSON displays tag info in JSON format
func (tc *TagDocCommand) displayJSON(t tag.Tag, info *tag.TagInfo) error {
	fmt.Printf("{\n")
	fmt.Printf("  \"tag\": \"%s\",\n", t.String())
	fmt.Printf("  \"hex\": \"%s\",\n", t.Hex())
	fmt.Printf("  \"name\": \"%s\",\n", info.Name)
	fmt.Printf("  \"keyword\": \"%s\",\n", info.Keyword)
	fmt.Printf("  \"vr\": \"%s\",\n", info.VR)
	fmt.Printf("  \"vm\": \"%s\",\n", info.VM)
	fmt.Printf("  \"retired\": %v,\n", info.Retired)
	fmt.Printf("  \"type\": \"%s\"\n", tagType(t))
	fmt.Printf("}\n")

	return nil
}

// generateTagDocumentation lists the dictionary, honoring the command's own flags.
//
// What it did before: printed "Displaying first 50 tags from the dictionary:", a
// column header, and then no tags at all — followed by three hardcoded counts. Then,
// as a side effect nobody asked for, it created dicom_tags_text.txt in the working
// directory. That file turned up committed to this repository once.
//
// So the command promised a listing, produced none, reported figures that were not
// measured, wrote a file that was not requested, and exited zero. -retired and
// -private were accepted and ignored.
//
// It now lists real entries from the dictionary, sorted by tag so the output is stable
// between runs, filters as the flags say, counts what it actually has, and writes to a
// file only when -output names one.
func (tc *TagDocCommand) generateTagDocumentation() error {
	type entry struct {
		tag  tag.Tag
		info *tag.TagInfo
	}

	entries := make([]entry, 0, len(tag.StandardDicomDictionary))
	retired := 0

	for value, info := range tag.StandardDicomDictionary {
		t := tag.Tag(value)

		if info.Retired {
			retired++
			if !tc.retired {
				continue
			}
		}
		if t.IsPrivate() && !tc.privateOK {
			continue
		}
		entries = append(entries, entry{tag: t, info: info})
	}

	// Map iteration order is random, so an unsorted listing would differ on every
	// run and could not be diffed or tested.
	sort.Slice(entries, func(i, j int) bool { return entries[i].tag < entries[j].tag })

	out := os.Stdout
	if tc.output != "" {
		file, err := os.Create(tc.output)
		if err != nil {
			return fmt.Errorf("tag-doc: creating %q: %w", tc.output, err)
		}
		defer func() { _ = file.Close() }()
		out = file
	}

	limit := tc.limit
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}

	switch tc.format {
	case "json":
		fmt.Fprintln(out, "[")
		for i, e := range entries[:limit] {
			comma := ","
			if i == limit-1 {
				comma = ""
			}
			fmt.Fprintf(out, "  {\"tag\": %q, \"name\": %q, \"keyword\": %q, \"vr\": %q, \"vm\": %q, \"retired\": %v}%s\n",
				e.tag.String(), e.info.Name, e.info.Keyword, e.info.VR, e.info.VM, e.info.Retired, comma)
		}
		fmt.Fprintln(out, "]")

	case "markdown":
		fmt.Fprintf(out, "# DICOM Tag Dictionary\n\n")
		fmt.Fprintf(out, "%d of %d tags.\n\n", limit, len(entries))
		fmt.Fprintln(out, "| Tag | Name | Keyword | VR | VM | Status |")
		fmt.Fprintln(out, "|-----|------|---------|----|----|--------|")
		for _, e := range entries[:limit] {
			fmt.Fprintf(out, "| `%s` | %s | `%s` | %s | %s | %s |\n",
				e.tag.String(), e.info.Name, e.info.Keyword, e.info.VR, e.info.VM, tagStatus(e.info))
		}

	default: // text
		fmt.Fprintln(out, "DICOM Tag Dictionary")
		fmt.Fprintln(out, strings.Repeat("=", 78))
		fmt.Fprintf(out, "\n%-13s %-34s %-4s %-6s %s\n", "Tag", "Name", "VR", "VM", "Status")
		fmt.Fprintln(out, strings.Repeat("-", 78))
		for _, e := range entries[:limit] {
			name := e.info.Name
			if len(name) > 34 {
				name = name[:31] + "..."
			}
			fmt.Fprintf(out, "%-13s %-34s %-4s %-6s %s\n",
				e.tag.String(), name, e.info.VR, e.info.VM, tagStatus(e.info))
		}
		fmt.Fprintln(out, strings.Repeat("-", 78))
		fmt.Fprintf(out, "shown %d of %d matching; dictionary holds %d standard tags "+
			"(%d retired) and %d repeater patterns\n",
			limit, len(entries), len(tag.StandardDicomDictionary), retired, len(tag.RepeaterDictionary))
		if limit < len(entries) {
			fmt.Fprintf(out, "use -limit 0 for all of them, or -output <file> to write them out\n")
		}
	}

	if tc.output != "" {
		fmt.Printf("wrote %d tags to %s\n", limit, tc.output)
	}
	return nil
}

// vrDescription provides human-readable description of VR types
func vrDescription(vr string) string {
	descriptions := map[string]string{
		"AE": "Application Entity",
		"AS": "Age String",
		"AT": "Attribute Tag",
		"CS": "Code String",
		"DA": "Date",
		"DS": "Decimal String",
		"DT": "Date/Time",
		"FD": "Floating Point Double",
		"FL": "Floating Point Single",
		"IS": "Integer String",
		"LO": "Long String",
		"LT": "Long Text",
		"OB": "Other Byte",
		"OD": "Other Double",
		"OF": "Other Float",
		"OL": "Other Long",
		"OW": "Other Word",
		"PN": "Person Name",
		"SH": "Short String",
		"SL": "Signed Long",
		"SQ": "Sequence of Items",
		"SS": "Signed Short",
		"ST": "Short Text",
		"TM": "Time",
		"UC": "Unlimited Characters",
		"UI": "Unique Identifier",
		"UL": "Unsigned Long",
		"UN": "Unknown",
		"UR": "Universal Resource Identifier",
		"UT": "Unlimited Text",
	}

	if desc, ok := descriptions[vr]; ok {
		return desc
	}
	return "Unknown"
}

// tagType returns the tag type (Standard or Private)
func tagType(t tag.Tag) string {
	if t.IsPrivate() {
		return "Private"
	}
	return "Standard"
}

// tagStatus returns the tag status (Active or Retired)
func tagStatus(info *tag.TagInfo) string {
	if info.Retired {
		return "Retired"
	}
	return "Active"
}

// showHelp displays help information for the tag-doc command.
func (tc *TagDocCommand) showHelp() {
	fmt.Print(`COMMAND: tag-doc - Generate tag documentation

USAGE:
  go-dicom tag-doc [options]

DESCRIPTION:
  Generate documentation for DICOM tags using the built-in 5,182-tag dictionary.
  Look up tag information by keyword or list all tags.

OPTIONS:
  -h, --help             Show help for this command
  --keyword <name>       Tag keyword to look up (e.g., PatientName)
  --format <format>      Output format: text, markdown, json (default: text)
  --retired              Include retired tags
  --private              Include private tags

EXAMPLES:
  go-dicom tag-doc --keyword PatientName
  go-dicom tag-doc --keyword PatientName --format markdown
  go-dicom tag-doc --keyword PatientName --format json
  go-dicom tag-doc
  go-dicom tag-doc -h
`)
}

// parseTagArgument reads a tag written the ways people write them.
//
// The dictionary is keyed by a 32-bit group/element pair, but nobody types that.
// DICOM tags appear in the wild as (0010,0010), 0010,0010 and 00100010 — the first
// two in every standard document and error message, the third in filenames and
// URLs — so all three are accepted rather than only the one the parser found easiest.
func parseTagArgument(s string) (tag.Tag, error) {
	cleaned := strings.TrimSpace(s)
	cleaned = strings.TrimPrefix(cleaned, "(")
	cleaned = strings.TrimSuffix(cleaned, ")")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")

	if len(cleaned) != 8 {
		return 0, fmt.Errorf("%q is not a DICOM tag: expected a group and element, "+
			"like 0010,0010 or (0010,0010) or 00100010", s)
	}

	value, err := strconv.ParseUint(cleaned, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("%q is not a DICOM tag: %q is not hexadecimal", s, cleaned)
	}

	return tag.New(uint16(value>>16), uint16(value&0xFFFF)), nil
}
