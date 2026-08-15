package cli

import "flag"

// ParseArgs parses flags that may appear anywhere among the positional arguments,
// and returns the positional ones in order.
//
// Go's flag package stops at the first argument that is not a flag, and treats
// everything after it as positional. So a command documented as
//
//	go-dicom convert patient.dcm data.csv --format csv
//
// parsed no flags at all: --format and its value became the third and fourth
// positional arguments, the format stayed at its default, and the command wrote JSON
// into a file called data.csv and exited zero. Nothing reported it. The same shape
// silently sent codify's output to stdout instead of the file named by --output.
//
// Both of those forms are in the CLI's own help. Rather than rewrite the help to
// describe the restriction, flags are accepted wherever they are written, which is
// what every user expects and what the help already promises.
//
// The loop is the usual permutation: parse until a positional stops it, set that one
// aside, and parse what remains. A "--" terminator is honored by flag.Parse itself,
// and everything after it is returned as positional.
func ParseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string

	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}

		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}

		// If flag.Parse stopped at "--" it has already consumed it, and everything
		// left is positional by the user's explicit instruction.
		if terminated(args, rest) {
			return append(positional, rest...), nil
		}

		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// terminated reports whether parsing stopped because of an explicit "--" rather than
// because it met an ordinary positional argument.
//
// flag.Parse consumes the "--" and returns what follows, so it is not in rest. The
// test is therefore whether the argument immediately before this run of leftovers, in
// the original slice, was the terminator.
func terminated(args, rest []string) bool {
	if len(rest) >= len(args) {
		return false
	}
	return args[len(args)-len(rest)-1] == "--"
}
