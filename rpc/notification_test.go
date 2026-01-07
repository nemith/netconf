package rpc

import (
	"context"
	"encoding/xml"
	"testing"
	"time"

	"github.com/carlmjohnson/be"
)

func TestCreateSubscriptionMarshalXML(t *testing.T) {
	tests := []struct {
		name    string
		sub     *CreateSubscription
		wantXML string
	}{
		{
			name:    "minimalSubscription",
			sub:     &CreateSubscription{},
			wantXML: `<create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"></create-subscription>`,
		},
		{
			name: "withStream",
			sub: &CreateSubscription{
				Stream: "NETCONF",
			},
			wantXML: `<create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"><stream>NETCONF</stream></create-subscription>`,
		},
		{
			name: "withFilter",
			sub: &CreateSubscription{
				Filter: SubtreeFilter("<interfaces/>"),
			},
			wantXML: `<create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"><filter type="subtree"><interfaces/></filter></create-subscription>`,
		},
		{
			name: "withStartTime",
			sub: func() *CreateSubscription {
				t := time.Date(2024, 1, 5, 12, 34, 56, 0, time.UTC)
				return &CreateSubscription{
					StartTime: &t,
				}
			}(),
			wantXML: `<create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"><startTime>2024-01-05T12:34:56Z</startTime></create-subscription>`,
		},
		{
			name: "withStartAndStopTime",
			sub: func() *CreateSubscription {
				start := time.Date(2024, 1, 5, 12, 0, 0, 0, time.UTC)
				stop := time.Date(2024, 1, 5, 13, 0, 0, 0, time.UTC)
				return &CreateSubscription{
					StartTime: &start,
					StopTime:  &stop,
				}
			}(),
			wantXML: `<create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"><startTime>2024-01-05T12:00:00Z</startTime><stopTime>2024-01-05T13:00:00Z</stopTime></create-subscription>`,
		},
		{
			name: "fullSubscription",
			sub: func() *CreateSubscription {
				start := time.Date(2024, 1, 5, 12, 0, 0, 0, time.UTC)
				return &CreateSubscription{
					Stream:    "NETCONF",
					Filter:    SubtreeFilter("<config/>"),
					StartTime: &start,
				}
			}(),
			wantXML: `<create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"><stream>NETCONF</stream><filter type="subtree"><config/></filter><startTime>2024-01-05T12:00:00Z</startTime></create-subscription>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := xml.Marshal(tt.sub)
			be.NilErr(t, err)
			be.Equal(t, tt.wantXML, string(data))
		})
	}
}

func TestCreateSubscriptionExec(t *testing.T) {
	tests := []struct {
		name        string
		op          CreateSubscription
		serverReply string
		shouldErr   bool
	}{
		{
			name:        "okReply",
			op:          CreateSubscription{},
			serverReply: `<ok/>`,
		},
		{
			name: "stopTimeWithoutStartTime",
			op: func() CreateSubscription {
				stopTime := time.Date(2024, 1, 5, 13, 0, 0, 0, time.UTC)
				return CreateSubscription{
					StopTime: &stopTime,
				}
			}(),
			shouldErr: true,
		},
		{
			name: "stopTimeBeforeStartTime",
			op: func() CreateSubscription {
				startTime := time.Date(2024, 1, 5, 14, 0, 0, 0, time.UTC)
				stopTime := time.Date(2024, 1, 5, 13, 0, 0, 0, time.UTC)
				return CreateSubscription{
					StartTime: &startTime,
					StopTime:  &stopTime,
				}
			}(),
			shouldErr: true,
		},
		{
			name:        "errorReply",
			op:          CreateSubscription{},
			serverReply: `<rpc-error><error-type>application</error-type><error-tag>operation-failed</error-tag><error-severity>error</error-severity><error-message>Subscription failed</error-message></rpc-error>`,
			shouldErr:   true,
		},
		{
			name:        "rpcErrorReply",
			op:          CreateSubscription{},
			serverReply: `<rpc-reply><rpc-error><error-type>application</error-type><error-tag>operation-failed</error-tag><error-severity>error</error-severity><error-message>Subscription failed</error-message></rpc-error></rpc-reply>`,
			shouldErr:   true,
		},
		{
			name:        "notOkReply",
			op:          CreateSubscription{},
			serverReply: `<data></data>`,
			shouldErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			session, _ := mockSession(t, tc.serverReply)

			err := tc.op.Exec(context.Background(), session)
			if tc.shouldErr {
				be.Nonzero(t, err)
			} else {
				be.NilErr(t, err)
			}
		})
	}
}
