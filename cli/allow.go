package cli

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/netip"
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
type allowFlag struct {
	titles   []string
	prefixes []netip.Prefix
}

func (a *allowFlag) add(fs *flag.FlagSet) {
	fs.Var(a, "allow",
		"Callers to admit, comma separated and repeatable: calling AE titles, and IP addresses "+
			"or CIDR ranges (10.0.0.0/8, fd00::/8). With both, a caller needs a listed title and "+
			"a listed address. Any other caller is refused before anything is negotiated: an "+
			"unlisted title as calling-AE-title-not-recognized, an unlisted address with no "+
			"reason given. A host name is not resolved: it is read as an AE title "+
			"(default: every caller is admitted)")
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
	if len(a.titles) == 0 && len(a.prefixes) == 0 {
		return nil
	}
	titles := make(map[string]bool, len(a.titles))
	for _, t := range a.titles {
		titles[t] = true
	}
	prefixes := append([]netip.Prefix{}, a.prefixes...)

	return func(_ context.Context, req *network.AssociationRequest) *network.AssociationRejection {
		// The address first, so a caller from a place none is admitted
		// learns nothing about which titles are known.
		if len(prefixes) > 0 && !addressAllowed(req.RemoteAddr, prefixes) {
			report("Refused %q from %s: the address is not on -allow\n", req.CallingAE, req.RemoteAddr)
			// PS3.8 Table 9-21 has no reason for an address. Saying the
			// calling AE title was not recognized would be untrue of a
			// listed title calling from the wrong place.
			return &network.AssociationRejection{
				Result: network.RJResultRejectedPermanent,
				Source: network.RJSourceServiceUser,
				Reason: network.RJReasonUserNoReason,
			}
		}
		// Compared as the SCP gives it, without the spaces that are not
		// significant, and with its case: PS3.5 does not fold AE titles.
		if len(titles) > 0 && !titles[req.CallingAE] {
			report("Refused %q from %s: the calling AE title is not on -allow\n", req.CallingAE, req.RemoteAddr)
			return network.RejectCallingAE()
		}
		return nil
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
