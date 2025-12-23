package rpc

import (
	"encoding/xml"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Helper struct for testing struct marshaling
type InterfaceFilter struct {
	XMLName xml.Name `xml:"interfaces"`
	Name    string   `xml:"interfaces>interface>name,omitempty"`
}

func TestSubtreeFilter_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		input    Filter
		expected string
	}{
		{
			name:     "string",
			input:    SubtreeFilter(`<users/>`),
			expected: `<root><filter type="subtree"><users/></filter></root>`,
		},
		{
			name:     "bytes",
			input:    SubtreeFilter([]byte(`<system/>`)),
			expected: `<root><filter type="subtree"><system/></filter></root>`,
		},
		{
			name:     "struct",
			input:    SubtreeFilter(InterfaceFilter{Name: "eth0"}),
			expected: `<root><filter type="subtree"><interfaces><interface><name>eth0</name></interface></interfaces></filter></root>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			wrapper := struct {
				XMLName xml.Name `xml:"root"`
				F       Filter   `xml:"filter"`
			}{F: tt.input}

			out, err := xml.Marshal(&wrapper)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, string(out))
		})
	}
}

func TestXPathFilter_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		input    Filter
		expected string
	}{
		{
			name:  "xpath",
			input: XPathFilter("/interfaces/interface/name", nil),
			// Note: Attributes order in map iteration is random, but here we have none.
			// Go's XML encoder usually alphabetizes attributes.
			expected: `<root><filter type="xpath" select="/interfaces/interface/name"></filter></root>`,
		},
		{
			name: "xpathNamespaces",
			input: XPathFilter(
				"/if:interfaces/if:interface",
				map[string]string{
					"if": "urn:ietf:params:xml:ns:yang:ietf-interfaces",
				},
			),
			// Expected outcome needs to check for the xmlns attribute.
			// Since map iteration order is random, exact string match might be flaky if we had multiple NS.
			// But with one NS, it's deterministic.
			expected: `<root><filter type="xpath" select="/if:interfaces/if:interface" xmlns:if="urn:ietf:params:xml:ns:yang:ietf-interfaces"></filter></root>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapper := struct {
				XMLName xml.Name `xml:"root"`
				F       Filter   `xml:"filter"`
			}{F: tt.input}

			out, err := xml.Marshal(&wrapper)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, string(out))
		})
	}
}
