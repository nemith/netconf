package rpc

import (
	"encoding/xml"
	"testing"

	"github.com/carlmjohnson/be"
)

func TestGetConfig_WithDefaults_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       GetConfig
		expected string
	}{
		{
			name: "without with-defaults",
			op: GetConfig{
				Source: Running,
			},
			expected: `<get-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><running/></source></get-config>`,
		},
		{
			name: "report-all",
			op: GetConfig{
				Source:       Running,
				WithDefaults: DefaultsReportAll,
			},
			expected: `<get-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><running/></source><with-defaults xmlns="urn:ietf:params:xml:ns:yang:ietf-netconf-with-defaults">report-all</with-defaults></get-config>`,
		},
		{
			name: "trim",
			op: GetConfig{
				Source:       Running,
				WithDefaults: DefaultsTrim,
			},
			expected: `<get-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><running/></source><with-defaults xmlns="urn:ietf:params:xml:ns:yang:ietf-netconf-with-defaults">trim</with-defaults></get-config>`,
		},
		{
			name: "explicit",
			op: GetConfig{
				Source:       Running,
				WithDefaults: DefaultsExplicit,
			},
			expected: `<get-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><running/></source><with-defaults xmlns="urn:ietf:params:xml:ns:yang:ietf-netconf-with-defaults">explicit</with-defaults></get-config>`,
		},
		{
			name: "report-all-tagged",
			op: GetConfig{
				Source:       Running,
				WithDefaults: DefaultsReportAllTagged,
			},
			expected: `<get-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><running/></source><with-defaults xmlns="urn:ietf:params:xml:ns:yang:ietf-netconf-with-defaults">report-all-tagged</with-defaults></get-config>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(tt.op)
			be.NilErr(t, err)
			be.Equal(t, tt.expected, string(got))
		})
	}
}

func TestGet_WithDefaults_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       Get
		expected string
	}{
		{
			name:     "without with-defaults",
			op:       Get{},
			expected: `<get xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"></get>`,
		},
		{
			name: "report-all",
			op: Get{
				WithDefaults: DefaultsReportAll,
			},
			expected: `<get xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><with-defaults xmlns="urn:ietf:params:xml:ns:yang:ietf-netconf-with-defaults">report-all</with-defaults></get>`,
		},
		{
			name: "trim",
			op: Get{
				WithDefaults: DefaultsTrim,
			},
			expected: `<get xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><with-defaults xmlns="urn:ietf:params:xml:ns:yang:ietf-netconf-with-defaults">trim</with-defaults></get>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(&tt.op)
			be.NilErr(t, err)
			be.Equal(t, tt.expected, string(got))
		})
	}
}

func TestCopyConfig_WithDefaults_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       CopyConfig
		expected string
	}{
		{
			name: "without with-defaults",
			op: CopyConfig{
				Source: Running,
				Target: Startup,
			},
			expected: `<copy-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><running/></source><target><startup/></target></copy-config>`,
		},
		{
			name: "with explicit",
			op: CopyConfig{
				Source:       Running,
				Target:       Startup,
				WithDefaults: DefaultsExplicit,
			},
			expected: `<copy-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><running/></source><target><startup/></target><with-defaults xmlns="urn:ietf:params:xml:ns:yang:ietf-netconf-with-defaults">explicit</with-defaults></copy-config>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(tt.op)
			be.NilErr(t, err)
			be.Equal(t, tt.expected, string(got))
		})
	}
}
