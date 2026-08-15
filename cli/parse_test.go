package cli

import (
	"flag"
	"strings"
	"testing"
)

// Flags must be honored wherever they are written.
//
// Go's flag package stops at the first non-flag argument, so a command documented as
//
//	go-dicom convert patient.dcm data.csv --format csv
//
// parsed no flags at all: --format and its value became positional arguments, the
// format stayed at its default, and the command wrote JSON into a file called
// data.csv and exited zero. The same shape sent codify's output to stdout rather than
// the file named by --output. Both forms are in the CLI's own help.
func TestFlagsAreHonouredWhereverTheyAppear(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		format   string
		wantArgs []string
	}{
		{
			name:     "flags first",
			args:     []string{"-format", "csv", "in.dcm", "out.csv"},
			format:   "csv",
			wantArgs: []string{"in.dcm", "out.csv"},
		},
		{
			name:     "flags last, as the help documents",
			args:     []string{"in.dcm", "out.csv", "-format", "csv"},
			format:   "csv",
			wantArgs: []string{"in.dcm", "out.csv"},
		},
		{
			name:     "double dash long form, last",
			args:     []string{"in.dcm", "out.csv", "--format", "csv"},
			format:   "csv",
			wantArgs: []string{"in.dcm", "out.csv"},
		},
		{
			name:     "interspersed",
			args:     []string{"in.dcm", "-format", "csv", "out.csv"},
			format:   "csv",
			wantArgs: []string{"in.dcm", "out.csv"},
		},
		{
			name:     "equals form",
			args:     []string{"in.dcm", "out.csv", "-format=csv"},
			format:   "csv",
			wantArgs: []string{"in.dcm", "out.csv"},
		},
		{
			name:     "no flags at all",
			args:     []string{"in.dcm", "out.json"},
			format:   "json",
			wantArgs: []string{"in.dcm", "out.json"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(&strings.Builder{})
			format := fs.String("format", "json", "")

			got, err := ParseArgs(fs, c.args)
			if err != nil {
				t.Fatalf("ParseArgs(%q): %v", c.args, err)
			}
			if *format != c.format {
				t.Errorf("format is %q, want %q — the flag was not applied", *format, c.format)
			}
			if strings.Join(got, ",") != strings.Join(c.wantArgs, ",") {
				t.Errorf("positional args %q, want %q", got, c.wantArgs)
			}
		})
	}
}

// A "--" terminator means the user has said explicitly that what follows is not a
// flag, and that must be respected — otherwise a file legitimately named "-format"
// could never be passed.
func TestADoubleDashStopsFlagParsing(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(&strings.Builder{})
	format := fs.String("format", "json", "")

	got, err := ParseArgs(fs, []string{"in.dcm", "--", "-format", "csv"})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if *format != "json" {
		t.Errorf("the flag after -- was applied (%q); -- must end flag parsing", *format)
	}
	want := []string{"in.dcm", "-format", "csv"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("positional args %q, want %q", got, want)
	}
}

// An unknown flag must still be an error, wherever it appears. Permuting the parse
// must not turn a typo into a silently ignored positional argument.
func TestAnUnknownFlagIsStillAnError(t *testing.T) {
	for _, args := range [][]string{
		{"-nosuchflag", "in.dcm"},
		{"in.dcm", "-nosuchflag"},
		{"in.dcm", "-nosuchflag", "out.csv"},
	} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		fs.SetOutput(&strings.Builder{})
		fs.String("format", "json", "")

		if _, err := ParseArgs(fs, args); err == nil {
			t.Errorf("ParseArgs(%q) accepted an undefined flag", args)
		}
	}
}

// Tags are written three ways in practice and all three must be accepted, because
// `tag-doc 0010,0010` is the form the CLI's own help and the README show.
func TestParseTagArgumentAcceptsHowTagsAreActuallyWritten(t *testing.T) {
	for _, in := range []string{"0010,0010", "(0010,0010)", "00100010", " 0010,0010 "} {
		got, err := parseTagArgument(in)
		if err != nil {
			t.Errorf("parseTagArgument(%q): %v", in, err)
			continue
		}
		if got.Group() != 0x0010 || got.Element() != 0x0010 {
			t.Errorf("parseTagArgument(%q) = (%04X,%04X), want (0010,0010)",
				in, got.Group(), got.Element())
		}
	}
}

// And junk must be refused with a message that says what a tag looks like, rather
// than being read as some other tag or silently listing the whole dictionary.
func TestParseTagArgumentRejectsJunk(t *testing.T) {
	for _, in := range []string{"", "not-a-tag", "0010", "0010,00100", "zzzz,zzzz"} {
		if _, err := parseTagArgument(in); err == nil {
			t.Errorf("parseTagArgument(%q) accepted junk", in)
		}
	}
}
