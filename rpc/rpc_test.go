package rpc

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nemith.io/netconf"
	"nemith.io/netconf/testutil"
)

func mockSession(t *testing.T, rpcReplyInnerXML string) (*netconf.Session, *testutil.Transport) {
	tr := testutil.NewTransport(func(req string) []string {
		// Respond to hello
		if strings.Contains(req, "<hello") {
			return []string{`
				<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<capabilities>
						<capability>urn:ietf:params:netconf:base:1.0</capability>
					</capabilities>
					<session-id>42</session-id>
				</hello>`}
		}
		// Respond to RPC
		msgID := testutil.ExtractMessageID(req)
		return []string{fmt.Sprintf(`
			<rpc-reply message-id="%s" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
				%s
			</rpc-reply>`, msgID, rpcReplyInnerXML)}
	})

	// Create Session
	// This will immediately consume the first message (Server Hello)
	// and write the Client Hello to tr.outputs[0].
	s, err := netconf.NewSession(tr)
	require.NoError(t, err)

	return s, tr
}

func TestUnmarshalOk(t *testing.T) {
	tt := []struct {
		name  string
		input string
		want  bool
	}{
		{"selfclosing", "<foo>><ok/></foo>", true},
		{"missing", "<foo></foo>", false},
		{"closetag", "<foo><ok></ok></foo>", true},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			var v struct {
				XMLName xml.Name   `xml:"foo"`
				Ok      ExtantBool `xml:"ok"`
			}

			err := xml.Unmarshal([]byte(tc.input), &v)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, bool(v.Ok))
		})
	}
}

func TestGet_MarshalXML(t *testing.T) {
	tests := []struct {
		name     string
		op       Get
		expected string
	}{
		{
			name:     "noFilter",
			op:       Get{},
			expected: `<get xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"></get>`,
		},
		{
			name: "withFilter",
			op: Get{
				Filter: SubtreeFilter(`<interfaces/>`),
			},
			expected: `<get xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><filter type="subtree"><interfaces/></filter></get>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := xml.Marshal(&tt.op)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(out))
		})
	}
}

func TestGet_Exec(t *testing.T) {
	const rpcReplyData = `<data><interfaces><interface><name>eth0</name></interface></interfaces></data>`

	sess, _ := mockSession(t, rpcReplyData)

	getOp := &Get{}
	data, err := getOp.Exec(context.Background(), sess)
	require.NoError(t, err)

	expectedData := `<interfaces><interface><name>eth0</name></interface></interfaces>`
	assert.Equal(t, expectedData, string(data))
}
