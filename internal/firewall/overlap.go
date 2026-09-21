package firewall

import (
	"net"
	"strings"
)

// AnnotateShadows walks each direction's own rules in evaluation (Order)
// order and marks every rule that comes after another rule which already
// matches every packet the later one would ever see. First match wins — the
// same left-to-right reading UFW/nft/iptables themselves already give their
// own rule list — so once an earlier, broader-or-equal rule exists, a later
// rule can never actually fire, no matter what its own Action says. Returns
// a copy; rules itself is left untouched.
func AnnotateShadows(rules []Rule) []Rule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]Rule, len(rules))
	copy(out, rules)
	for i := range out {
		for j := range out {
			if out[j].Direction != out[i].Direction || out[j].Order >= out[i].Order {
				continue
			}
			if covers(out[j], out[i]) {
				out[i].ShadowedByOrder = out[j].Order
				out[i].ShadowedByAction = out[j].Action
				break
			}
		}
	}
	return out
}

// covers reports whether every packet b would ever match is also matched by
// a — a restricts nothing that b doesn't already restrict at least as
// broadly, on every one of protocol, port, source, destination, interface.
func covers(a, b Rule) bool {
	if a.Protocol != "" && !strings.EqualFold(a.Protocol, b.Protocol) {
		return false
	}
	if !portsCover(a, b) {
		return false
	}
	if a.Source != "" && !cidrCovers(a.Source, b.Source) {
		return false
	}
	if a.Destination != "" && !cidrCovers(a.Destination, b.Destination) {
		return false
	}
	if a.Interface != "" && a.Interface != b.Interface {
		return false
	}
	return true
}

func portsCover(a, b Rule) bool {
	if a.HasAnyPort() {
		return true
	}
	if b.HasAnyPort() {
		return false // a restricts to a port range, b doesn't — b also matches ports outside it
	}
	return a.PortFrom <= b.PortFrom && b.PortTo <= a.PortTo
}

// cidrCovers reports whether every address b (a single IP or a CIDR) itself
// covers is also inside a (a single IP or CIDR). b == "" ("any address") is
// never covered by a's own narrower restriction — covers itself only calls
// this once a is already known to restrict the source/destination, so an
// unrestricted b would still match addresses a doesn't.
func cidrCovers(a, b string) bool {
	if b == "" {
		return false
	}
	aNet, err := toIPNet(a)
	if err != nil {
		return false
	}
	bNet, err := toIPNet(b)
	if err != nil {
		return false
	}
	aOnes, aBits := aNet.Mask.Size()
	bOnes, bBits := bNet.Mask.Size()
	if aBits != bBits || aOnes > bOnes {
		return false
	}
	return aNet.Contains(bNet.IP)
}

// toIPNet reads either a bare IP address (treated as a /32 or /128 — that
// exact single host) or a CIDR into a net.IPNet.
func toIPNet(s string) (*net.IPNet, error) {
	if !strings.Contains(s, "/") {
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, &net.ParseError{Type: "IP address", Text: s}
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		return &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}, nil
	}
	_, ipnet, err := net.ParseCIDR(s)
	return ipnet, err
}
