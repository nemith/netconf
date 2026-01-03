package rpc

import (
	"encoding/xml"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarshalDatastore(t *testing.T) {
	tt := []struct {
		input     Datastore
		want      string
		shouldErr bool
	}{
		{Running, "<rpc><target><running/></target></rpc>", false},
		{Startup, "<rpc><target><startup/></target></rpc>", false},
		{Candidate, "<rpc><target><candidate/></target></rpc>", false},
		{Datastore("custom-store"), "<rpc><target><custom-store/></target></rpc>", false},
		{Datastore(""), "", true},
		{Datastore("<xml-elements>"), "", true},
	}

	for _, tc := range tt {
		t.Run(string(tc.input), func(t *testing.T) {
			v := struct {
				XMLName xml.Name  `xml:"rpc"`
				Target  Datastore `xml:"target"`
			}{Target: tc.input}

			got, err := xml.Marshal(&v)
			if !tc.shouldErr {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestGetConfig_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       GetConfig
		expected string
	}{
		{
			name: "basic",
			op: GetConfig{
				Source: Running,
			},
			expected: `<get-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><running/></source></get-config>`,
		},
		{
			name: "subtreeFilter",
			op: GetConfig{
				Source: Running,
				Filter: SubtreeFilter(`<users/>`),
			},
			expected: `<get-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><running/></source><filter type="subtree"><users/></filter></get-config>`,
		},
		{
			name: "xpathFilter",
			op: GetConfig{
				Source: Running,
				Filter: XPathFilter("/interfaces/interface/name", nil),
			},
			expected: `<get-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><running/></source><filter type="xpath" select="/interfaces/interface/name"></filter></get-config>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(tt.op)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			assert.Equal(t, tt.expected, string(got))
		})
	}
}
func TestGetConfig_Exec(t *testing.T) {
	tests := []struct {
		name        string
		op          GetConfig
		serverReply string
		shouldError bool
		expected    string
	}{
		{
			name: "good reply",
			op: GetConfig{
				Source: Datastore("my-datastore"),
			},
			serverReply: `<data><top xmlns="http://example.com/schema/1.2/config"><users><user><name>root</name></user></users></top></data>`,
			shouldError: false,
			expected:    `<top xmlns="http://example.com/schema/1.2/config"><users><user><name>root</name></user></users></top>`,
		},
	}

	for _, tc := range tests {
		session, _ := mockSession(t, tc.serverReply)
		got, err := tc.op.Exec(t.Context(), session)
		assert.NoError(t, err)
		expected := `<top xmlns="http://example.com/schema/1.2/config"><users><user><name>root</name></user></users></top>`
		assert.Equal(t, expected, string(got))

	}

}

func TestEditConfig_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       EditConfig
		expected string
	}{
		{
			name: "stringConfig",
			op: EditConfig{
				Target: Running,
				Config: `<interface><name>eth0</name></interface>`,
			},
			// Expect: <config>...content...</config>
			expected: `<edit-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><target><running/></target><config><interface><name>eth0</name></interface></config></edit-config>`,
		},
		{
			name: "byteSliceConfig",
			op: EditConfig{
				Target: Running,
				Config: []byte(`<interface><name>eth0</name></interface>`),
			},
			// Expect: <config>...content...</config>
			expected: `<edit-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><target><running/></target><config><interface><name>eth0</name></interface></config></edit-config>`,
		},
		{
			name: "urlConfig",
			op: EditConfig{
				Target: Candidate,
				Config: URL("https://example.com/config.xml"),
			},
			// Expect: <url>...</url> NOT wrapped in <config>
			expected: `<edit-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><target><candidate/></target><url>https://example.com/config.xml</url></edit-config>`,
		},
		{
			name: "optionsSet",
			op: EditConfig{
				Target:           Running,
				DefaultOperation: ReplaceConfig,
				TestOption:       TestThenSet,
				ErrorOption:      RollbackOnError,
				Config:           "foo",
			},
			expected: `<edit-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><target><running/></target><default-operation>replace</default-operation><test-option>test-then-set</test-option><error-option>rollback-on-error</error-option><config>foo</config></edit-config>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(tt.op)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			assert.Equal(t, tt.expected, string(got))
		})
	}
}

func TestEditConfig_Exec(t *testing.T) {
	tests := []struct {
		name        string
		op          EditConfig
		serverReply string
		shouldError bool
	}{
		{
			name: "okReply",
			op: EditConfig{
				Target: Running,
				Config: `<interface><name>eth0</name></interface>`,
			},
			serverReply: `<ok/>`,
			shouldError: false,
		},
	}

	for _, tc := range tests {
		session, _ := mockSession(t, tc.serverReply)
		err := tc.op.Exec(t.Context(), session)
		if tc.shouldError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestCopyConfig_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       CopyConfig
		expected string
	}{
		{
			name: "basic",
			op: CopyConfig{
				Source: URL("ftp://example.com/config.xml"),
				Target: Running,
			},
			expected: `<copy-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><url>ftp://example.com/config.xml</url></source><target><running/></target></copy-config>`,
		},
		{
			name: "withDefault",
			op: CopyConfig{
				Source: Startup,
				Target: Candidate,
			},
			expected: `<copy-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><startup/></source><target><candidate/></target></copy-config>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(tt.op)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			assert.Equal(t, tt.expected, string(got))
		})
	}
}

func TestCopyConfig_Exec(t *testing.T) {
	tests := []struct {
		name        string
		op          CopyConfig
		serverReply string
		shouldError bool
	}{
		{
			name: "okReply",
			op: CopyConfig{
				Source: URL("ftp://example.com/config.xml"),
				Target: Running,
			},
			serverReply: `<ok/>`,
			shouldError: false,
		},
	}

	for _, tc := range tests {
		session, _ := mockSession(t, tc.serverReply)
		err := tc.op.Exec(t.Context(), session)
		if tc.shouldError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestDeleteConfig_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       DeleteConfig
		expected string
	}{
		{
			name: "basic",
			op: DeleteConfig{
				Target: Datastore("my-custom-datastore"),
			},
			expected: `<delete-config xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><target><my-custom-datastore/></target></delete-config>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(tt.op)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			assert.Equal(t, tt.expected, string(got))
		})
	}
}

func TestDeleteConfig_Exec(t *testing.T) {
	tests := []struct {
		name        string
		op          DeleteConfig
		serverReply string
		shouldError bool
	}{
		{
			name: "okReply",
			op: DeleteConfig{
				Target: Datastore("my-custom-datastore"),
			},
			serverReply: `<ok/>`,
			shouldError: false,
		},
	}

	for _, tc := range tests {
		session, _ := mockSession(t, tc.serverReply)
		err := tc.op.Exec(t.Context(), session)
		if tc.shouldError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestLock_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       Lock
		expected string
	}{
		{
			name: "basic",
			op: Lock{
				Target: Running,
			},
			expected: `<lock xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><target><running/></target></lock>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(tt.op)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			assert.Equal(t, tt.expected, string(got))
		})
	}
}

func TestLock_Exec(t *testing.T) {
	tests := []struct {
		name        string
		op          Lock
		serverReply string
		shouldError bool
	}{
		{
			name: "okReply",
			op: Lock{
				Target: Running,
			},
			serverReply: `<ok/>`,
			shouldError: false,
		},
	}

	for _, tc := range tests {
		session, _ := mockSession(t, tc.serverReply)
		err := tc.op.Exec(t.Context(), session)
		if tc.shouldError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestUnlock_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       Unlock
		expected string
	}{
		{
			name: "basic",
			op: Unlock{
				Target: Running,
			},
			expected: `<unlock xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><target><running/></target></unlock>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(tt.op)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			assert.Equal(t, tt.expected, string(got))
		})
	}
}

func TestUnlock_Exec(t *testing.T) {
	tests := []struct {
		name        string
		op          Unlock
		serverReply string
		shouldError bool
	}{
		{
			name: "okReply",
			op: Unlock{
				Target: Running,
			},
			serverReply: `<ok/>`,
			shouldError: false,
		},
	}

	for _, tc := range tests {
		session, _ := mockSession(t, tc.serverReply)
		err := tc.op.Exec(t.Context(), session)
		if tc.shouldError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestValidate_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       Validate
		expected string
	}{
		{
			name: "basic",
			op: Validate{
				Source: Running,
			},
			expected: `<validate xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><source><running/></source></validate>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(tt.op)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			assert.Equal(t, tt.expected, string(got))
		})
	}
}

func TestValidate_Exec(t *testing.T) {
	tests := []struct {
		name        string
		op          Validate
		serverReply string
		shouldError bool
	}{
		{
			name: "okReply",
			op: Validate{
				Source: Running,
			},
			serverReply: `<ok/>`,
			shouldError: false,
		},
	}

	for _, tc := range tests {
		session, _ := mockSession(t, tc.serverReply)
		err := tc.op.Exec(t.Context(), session)
		if tc.shouldError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestCommit_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       Commit
		expected string
	}{
		{
			name:     "basic",
			op:       Commit{},
			expected: `<commit xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"></commit>`,
		},
		{
			name: "confirmed",
			op: Commit{
				Confirmed:      true,
				ConfirmTimeout: 300,
			},
			expected: `<commit xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><confirmed></confirmed><confirm-timeout>300</confirm-timeout></commit>`,
		},
		{
			name: "confirmedPersist",
			op: Commit{
				Confirmed: true,
				PersistID: "foobar",
			},
			expected: `<commit xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><confirmed></confirmed><persist>foobar</persist></commit>`,
		},
		{
			name: "confirmPersistID",
			op: Commit{
				PersistID: "foobar2",
			},
			expected: `<commit xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><persist-id>foobar2</persist-id></commit>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(tt.op)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			assert.Equal(t, tt.expected, string(got))
		})
	}
}

func TestCommit_Exec(t *testing.T) {
	tests := []struct {
		name        string
		op          Commit
		serverReply string
		shouldError bool
	}{
		{
			name: "okReply",
			op: Commit{
				Confirmed:      true,
				ConfirmTimeout: 200,
			},
			serverReply: `<ok/>`,
			shouldError: false,
		},
	}

	for _, tc := range tests {
		session, _ := mockSession(t, tc.serverReply)
		err := tc.op.Exec(t.Context(), session)
		if tc.shouldError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestCancelCommit_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       CancelCommit
		expected string
	}{
		{
			name:     "basic",
			op:       CancelCommit{},
			expected: `<cancel-commit xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"></cancel-commit>`,
		},
		{
			name: "persistID",
			op: CancelCommit{
				PersistID: "persist-123",
			},
			expected: `<cancel-commit xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><persist-id>persist-123</persist-id></cancel-commit>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := xml.Marshal(tt.op)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			assert.Equal(t, tt.expected, string(got))
		})
	}
}

func TestCancelCommit_Exec(t *testing.T) {
	tests := []struct {
		name        string
		op          CancelCommit
		serverReply string
		shouldError bool
	}{
		{
			name:        "okReply",
			op:          CancelCommit{},
			serverReply: `<ok/>`,
			shouldError: false,
		},
	}

	for _, tc := range tests {
		session, _ := mockSession(t, tc.serverReply)
		err := tc.op.Exec(t.Context(), session)
		if tc.shouldError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}
