package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/amrshadid/go-dicom/network"
)

// allowFlag is the -allow list of storescp and qrscp: the callers a server
// admits, by calling AE title, by address, or both. Without it a server
// answers anyone who reaches its port and names its AE title.
//
// An entry that reads as an IP address or a CIDR range is an address; any
// other is a calling AE title. When both kinds are given a caller needs one
// of each: "-allow PRISMA1,10.0.0.0/8" admits PRISMA1 calling from
// 10.0.0.0/8, and neither PRISMA1 from elsewhere nor another title from
// there. Entries of one kind are alternatives. A host name is refused rather
// than resolved: resolved once at start-up it would hold whatever the name
// pointed to then, and resolved on every association it would put DNS in
// front of every caller.
//
// An entry that pairs a title with an address, TITLE@ADDRESS, is a third
// kind: that title is admitted from that address, and from nowhere else
// unless another entry says so. Each pair is a way in of its own, beside the
// way in the titles and addresses make together: "-allow CT2,10.0.0.0/8,
// PRISMA1@192.168.1.5" admits CT2 from 10.0.0.0/8 and PRISMA1 from
// 192.168.1.5, and neither CT2 from 192.168.1.5 nor PRISMA1 from 10.0.0.0/8.
// An AE title may hold an @ (PS3.5 6.2), so an entry is a pair only when
// what follows its last @ is an address.
//
// The same entries may be read from a file, -allow-file, one to a line.
type allowFlag struct {
	titles   []string
	prefixes []netip.Prefix
	pairs    []allowPair
}

// allowPair is a title admitted from an address or a range.
type allowPair struct {
	title  string
	prefix netip.Prefix
}

func (p allowPair) String() string { return p.title + "@" + p.prefix.String() }

func (a *allowFlag) add(fs *flag.FlagSet) {
	fs.Var(a, "allow",
		"Callers to admit, comma separated and repeatable: calling AE titles, IP addresses "+
			"or CIDR ranges (10.0.0.0/8, fd00::/8), and TITLE@ADDRESS to admit a title only from "+
			"an address (PRISMA1@10.0.0.5). With titles and addresses both, a caller needs a "+
			"listed title and a listed address; each TITLE@ADDRESS is a way in of its own. Any "+
			"other caller is refused before anything is negotiated: an unlisted title as "+
			"calling-AE-title-not-recognized, an unlisted address, or a listed title from an "+
			"address it is not admitted from, with no reason given. A host name is not "+
			"resolved: it is read as an AE title (default: every caller is admitted)")
	fs.Var(allowFileFlag{a}, "allow-file",
		"A file of entries for -allow, one to a line, in the same forms; a # starts a comment, "+
			"on a line of its own or after a space. Repeatable, and joined with -allow. "+
			"A file with no entry is an error: it would admit every caller")
}

// allowFileFlag is -allow-file: it reads the entries of a file into the list.
type allowFileFlag struct{ list *allowFlag }

func (f allowFileFlag) String() string { return "" }

func (f allowFileFlag) Set(path string) error { return f.list.readFile(path) }

// maxAllowFile bounds what -allow-file reads. A list of callers is a few
// lines a modality; a megabyte is a file that is not one.
const maxAllowFile = 1 << 20

// readFile adds the entries of a file, one to a line. A # at the start of a
// line, or after a space or a tab, begins a comment that runs to its end, so
// an AE title that holds " #" cannot be written here and is given with
// -allow. A file with no entry is refused: an empty list admits every
// caller, and a file emptied by mistake or named wrongly would open the
// server.
func (a *allowFlag) readFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("-allow-file: %v", err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(&limitedReader{r: f, n: maxAllowFile + 1})
	entries := 0
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimRight(scanner.Text(), "\r")
		if line == 1 {
			text = strings.TrimPrefix(text, "\uFEFF")
		}
		if i := commentStart(text); i >= 0 {
			text = text[:i]
		}
		if strings.Trim(text, " \t") == "" {
			continue
		}
		before := len(a.titles) + len(a.prefixes) + len(a.pairs)
		if err := a.Set(strings.ReplaceAll(text, "\t", " ")); err != nil {
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
		entries += len(a.titles) + len(a.prefixes) + len(a.pairs) - before
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("-allow-file %s: %v", path, err)
	}
	if entries == 0 {
		return fmt.Errorf("-allow-file %s has no entry; an empty list admits every caller", path)
	}
	return nil
}

// limitedReader fails, rather than ends, when it has given n bytes, so a file
// over the bound is an error and not a list that stops early.
type limitedReader struct {
	r io.Reader
	n int
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, fmt.Errorf("the file is longer than %d bytes", maxAllowFile)
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= n
	return n, err
}

// commentStart is where a comment begins in a line, or -1.
func commentStart(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return i
		}
	}
	return -1
}

// String lists the entries, titles first, in a form Set reads back.
func (a *allowFlag) String() string {
	if a == nil {
		return ""
	}
	entries := append([]string{}, a.titles...)
	for _, p := range a.prefixes {
		entries = append(entries, p.String())
	}
	for _, p := range a.pairs {
		entries = append(entries, p.String())
	}
	return strings.Join(entries, ",")
}

// describe says how the entries were read, for the line a server prints when
// it starts: a host name is taken for an AE title, and this is where that
// shows.
func (a *allowFlag) describe() string {
	var parts []string
	if len(a.titles) > 0 {
		parts = append(parts, "AE titles "+strings.Join(a.titles, ", "))
	}
	if len(a.prefixes) > 0 {
		ranges := make([]string, len(a.prefixes))
		for i, p := range a.prefixes {
			ranges[i] = p.String()
		}
		parts = append(parts, "addresses "+strings.Join(ranges, ", "))
	}
	if len(a.pairs) > 0 {
		pairs := make([]string, len(a.pairs))
		for i, p := range a.pairs {
			pairs[i] = p.String()
		}
		parts = append(parts, "titles from addresses "+strings.Join(pairs, ", "))
	}
	return strings.Join(parts, "; ")
}

// Set adds a comma-separated list of entries. The flag may be given more than
// once, and the lists are joined.
func (a *allowFlag) Set(value string) error {
	for _, entry := range strings.Split(value, ",") {
		// Spaces at either end of an AE title are not significant (PS3.5
		// 6.2), and none belongs in an address.
		entry = strings.Trim(entry, " ")
		if entry == "" {
			continue
		}
		if prefix, ok := parseAllowedAddress(entry); ok {
			a.prefixes = append(a.prefixes, prefix)
			continue
		}
		if at := strings.LastIndex(entry, "@"); at >= 0 {
			title, tail := strings.Trim(entry[:at], " "), strings.Trim(entry[at+1:], " ")
			if prefix, ok := parseAllowedAddress(tail); ok {
				if title == "" {
					return fmt.Errorf("-allow entry %q has no AE title before the @", entry)
				}
				if err := checkAllowedTitle(title); err != nil {
					return err
				}
				a.pairs = append(a.pairs, allowPair{title: title, prefix: prefix})
				continue
			}
			// What follows reads as an address and is not one: a pairing
			// mistyped, which as a title would admit no one.
			if tail != "" && strings.Trim(tail, "0123456789./:") == "" {
				return fmt.Errorf("-allow entry %q: %q after the @ is not a valid IP address or CIDR range", entry, tail)
			}
		}
		if err := checkAllowedTitle(entry); err != nil {
			return err
		}
		a.titles = append(a.titles, entry)
	}
	return nil
}

// parseAllowedAddress reads an entry as an address or a CIDR range. A single
// address is the range of its full length.
func parseAllowedAddress(entry string) (netip.Prefix, bool) {
	if prefix, err := netip.ParsePrefix(entry); err == nil {
		// 10.1.2.3/8 is the range 10.0.0.0/8, not the one host it reads as.
		// An IPv4 range written in its IPv6-mapped form is kept as IPv4,
		// since that is how a caller's address is compared.
		if prefix.Addr().Is4In6() && prefix.Bits() >= 96 {
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		return prefix.Masked(), true
	}
	if addr, err := netip.ParseAddr(entry); err == nil && addr.Zone() == "" {
		addr = addr.Unmap()
		return netip.PrefixFrom(addr, addr.BitLen()), true
	}
	return netip.Prefix{}, false
}

// checkAllowedTitle refuses an entry that is neither an address nor an AE
// title (PS3.5 6.2: 1 to 16 characters of the default repertoire, with no
// control character and no backslash). Such an entry would sit in the list
// admitting no one, and a mistyped address would read as a title.
func checkAllowedTitle(entry string) error {
	if strings.ContainsAny(entry, "/:") || (strings.Trim(entry, "0123456789.") == "" && strings.Contains(entry, ".")) {
		// No scanner's AE title looks like this, and a mistyped address does.
		return fmt.Errorf("-allow entry %q is not a valid IP address or CIDR range", entry)
	}
	if len(entry) > 16 {
		return fmt.Errorf("-allow entry %q is not an AE title: longer than 16 characters", entry)
	}
	for i := 0; i < len(entry); i++ {
		if c := entry[i]; c < 0x20 || c > 0x7E || c == '\\' {
			return fmt.Errorf("-allow entry %q is not an AE title: it holds %q", entry, rune(c))
		}
	}
	return nil
}

// admission returns the function for SCPConfig.Admit, or nil when the list is
// empty, so that a server started without -allow is what it always was. Each
// refusal is reported to report: a command prints its own lines and not the
// library's log (#283), so without it a refused scanner would leave no trace
// on the server's side.
func (a *allowFlag) admission(report func(format string, args ...any)) func(context.Context, *network.AssociationRequest) *network.AssociationRejection {
	if len(a.titles) == 0 && len(a.prefixes) == 0 && len(a.pairs) == 0 {
		return nil
	}
	titles := make(map[string]bool, len(a.titles))
	for _, t := range a.titles {
		titles[t] = true
	}
	prefixes := append([]netip.Prefix{}, a.prefixes...)
	pairs := append([]allowPair{}, a.pairs...)
	// Every range there is, for telling a caller from an address no entry
	// names from one that is named for another title.
	listed := append([]netip.Prefix{}, prefixes...)
	pairedTitles := make(map[string]bool, len(pairs))
	for _, p := range pairs {
		listed = append(listed, p.prefix)
		pairedTitles[p.title] = true
	}
	// The titles and addresses together are one way in, with the rule they
	// have always had: both kinds, when both are given. Each pair is another.
	together := len(titles) > 0 || len(prefixes) > 0

	return func(_ context.Context, req *network.AssociationRequest) *network.AssociationRejection {
		// Compared as the SCP gives it, without the spaces that are not
		// significant, and with its case: PS3.5 does not fold AE titles.
		if together &&
			(len(prefixes) == 0 || addressAllowed(req.RemoteAddr, prefixes)) &&
			(len(titles) == 0 || titles[req.CallingAE]) {
			return nil
		}
		for _, p := range pairs {
			if p.title == req.CallingAE && addressAllowed(req.RemoteAddr, []netip.Prefix{p.prefix}) {
				return nil
			}
		}

		noReason := &network.AssociationRejection{
			Result: network.RJResultRejectedPermanent,
			Source: network.RJSourceServiceUser,
			Reason: network.RJReasonUserNoReason,
		}
		switch {
		case pairedTitles[req.CallingAE]:
			// A listed title, from an address it is not admitted from. The
			// table has no reason for that, and not recognizing a title that
			// is listed would be untrue.
			report("Refused %q from %s: the address is not one %q is admitted from on -allow\n", req.CallingAE, req.RemoteAddr, req.CallingAE)
			return noReason
		case titles[req.CallingAE]:
			report("Refused %q from %s: the address is not on -allow\n", req.CallingAE, req.RemoteAddr)
			return noReason
		case len(listed) > 0 && !addressAllowed(req.RemoteAddr, listed):
			// The address first, so a caller from a place none is admitted
			// learns nothing about which titles are known.
			report("Refused %q from %s: the address is not on -allow\n", req.CallingAE, req.RemoteAddr)
			// PS3.8 Table 9-21 has no reason for an address. Saying the
			// calling AE title was not recognized would be untrue of a
			// listed title calling from the wrong place.
			return noReason
		}
		report("Refused %q from %s: the calling AE title is not on -allow\n", req.CallingAE, req.RemoteAddr)
		return network.RejectCallingAE()
	}
}

// addressAllowed reports whether a caller's address is in one of the ranges.
// An IPv4 caller reaching a dual-stack listener arrives as an IPv4-mapped IPv6
// address, and is compared as the IPv4 address it is.
func addressAllowed(remote net.Addr, prefixes []netip.Prefix) bool {
	var addr netip.Addr
	if tcp, ok := remote.(*net.TCPAddr); ok {
		addr = tcp.AddrPort().Addr()
	} else if remote != nil {
		ap, err := netip.ParseAddrPort(remote.String())
		if err != nil {
			return false
		}
		addr = ap.Addr()
	}
	if !addr.IsValid() {
		return false
	}
	addr = addr.Unmap().WithZone("")
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// printRefusal is the report a command gives admission: its refusals on its
// own output, among the lines for what it received.
func printRefusal(format string, args ...any) { fmt.Printf(format, args...) }
