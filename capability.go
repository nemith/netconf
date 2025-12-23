package netconf

import "iter"

const (
	baseCap      = "urn:ietf:params:netconf:base"
	stdCapPrefix = "urn:ietf:params:netconf:capability"

	CapNetConfig10 = baseCap + ":1.0"
	CapNetConfig11 = baseCap + ":1.1"
)

// DefaultCapabilities are the capabilities sent by the client during the hello
// exchange by the server.
var DefaultCapabilities = []string{
	CapNetConfig10,
	CapNetConfig11,

	// XXX: these seems like server capabilities and i don't see why
	// a client would need to send them

	// "urn:ietf:params:netconf:capability:writable-running:1.0",
	// "urn:ietf:params:netconf:capability:candidate:1.0",
	// "urn:ietf:params:netconf:capability:confirmed-commit:1.0",
	// "urn:ietf:params:netconf:capability:rollback-on-error:1.0",
	// "urn:ietf:params:netconf:capability:startup:1.0",
	// "urn:ietf:params:netconf:capability:url:1.0?scheme=http,ftp,file,https,sftp",
	// "urn:ietf:params:netconf:capability:validate:1.0",
	// "urn:ietf:params:netconf:capability:xpath:1.0",
	// "urn:ietf:params:netconf:capability:notification:1.0",
	// "urn:ietf:params:netconf:capability:interleave:1.0",
	// "urn:ietf:params:netconf:capability:with-defaults:1.0",
}

// ExpandCapability will automatically add the standard capability prefix of
// `urn:ietf:params:netconf:capability` if not already present.
func ExpandCapability(s string) string {
	if s == "" {
		return ""
	}

	if s[0] != ':' {
		return s
	}

	return stdCapPrefix + s
}

// XXX: may want to expose this type publicly in the future when the api has
// stabilized?
type CapabilitySet struct {
	caps map[string]struct{}
}

func NewCapabilitySet(capabilities ...string) CapabilitySet {
	cs := CapabilitySet{
		caps: make(map[string]struct{}),
	}
	cs.Add(capabilities...)
	return cs
}

func (cs *CapabilitySet) Add(capabilities ...string) {
	for _, cap := range capabilities {
		cap = ExpandCapability(cap)
		cs.caps[cap] = struct{}{}
	}
}

func (cs CapabilitySet) Has(s string) bool {
	// XXX: need to figure out how to handle versions (i.e always map to 1.0 or
	// map to latest/any?)
	s = ExpandCapability(s)
	_, ok := cs.caps[s]
	return ok
}

func (cs CapabilitySet) Remove(capabilities ...string) {
	for _, cap := range capabilities {
		cap = ExpandCapability(cap)
		delete(cs.caps, cap)
	}
}

func (cs CapabilitySet) All() iter.Seq[string] {
	return func(yield func(string) bool) {
		for cap := range cs.caps {
			if !yield(cap) {
				return
			}
		}
	}
}

func (cs CapabilitySet) Len() int {
	return len(cs.caps)
}
