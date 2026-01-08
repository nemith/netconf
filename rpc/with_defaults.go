package rpc

import "encoding/xml"

// WithDefaultsMode specifies how default values should be reported
// as defined in RFC 6243.
type WithDefaultsMode string

const (
	// DefaultsReportAll returns all data nodes including those set to their
	// schema default values.
	DefaultsReportAll WithDefaultsMode = "report-all"

	// DefaultsReportAllTagged returns all data nodes, with default nodes
	// marked with a default="true" attribute.
	DefaultsReportAllTagged WithDefaultsMode = "report-all-tagged"

	// DefaultsTrim omits data nodes set to their schema default values.
	DefaultsTrim WithDefaultsMode = "trim"

	// DefaultsExplicit reports only nodes that have been explicitly set
	// by the client, plus any state data.
	DefaultsExplicit WithDefaultsMode = "explicit"
)

// withDefaultsElement is a helper for marshaling the with-defaults element.
type withDefaultsElement struct {
	XMLName xml.Name         `xml:"urn:ietf:params:xml:ns:yang:ietf-netconf-with-defaults with-defaults"`
	Mode    WithDefaultsMode `xml:",chardata"`
}
