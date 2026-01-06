package rpc

import (
	"context"
	"encoding/xml"
	"fmt"
	"time"

	"nemith.io/netconf"
)

// CreateSubscription represents the `<create-subscription>` operation defined
// in [RFC 5277 Section 2.1.1] for initiating a notification subscription.
//
// The subscription will cause the server to begin sending event notifications
// to the client as they occur. If StartTime is specified, the server will first
// replay historical events from that time (if supported).
//
// [RFC 5277 Section 2.1.1]: https://www.rfc-editor.org/rfc/rfc5277.html#section-2.1.1
type CreateSubscription struct {
	// Stream indicates which notification stream to subscribe to.
	// If empty, the default NETCONF stream is used.
	Stream string

	// Filter selects a subset of notifications.
	Filter Filter

	// StartTime triggers replay of notifications from the specified time.
	// Requires server support for notification replay.
	StartTime *time.Time

	// StopTime automatically terminates the subscription at the specified time.
	// Requires StartTime to be set.
	StopTime *time.Time
}

func (rpc *CreateSubscription) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	createSub := struct {
		XMLName   xml.Name   `xml:"urn:ietf:params:xml:ns:netconf:notification:1.0 create-subscription"`
		Stream    string     `xml:"stream,omitempty"`
		Filter    Filter     `xml:"filter,omitempty"`
		StartTime *time.Time `xml:"startTime,omitempty"`
		StopTime  *time.Time `xml:"stopTime,omitempty"`
	}{
		Stream:    rpc.Stream,
		Filter:    rpc.Filter,
		StartTime: rpc.StartTime,
		StopTime:  rpc.StopTime,
	}

	return e.Encode(createSub)
}

func (rpc *CreateSubscription) Exec(ctx context.Context, session *netconf.Session) error {
	if rpc.StopTime != nil {
		if rpc.StartTime == nil {
			return fmt.Errorf("create-subscription: StopTime specified without StartTime")
		}
		if rpc.StopTime.Before(*rpc.StartTime) {
			return fmt.Errorf("create-subscription: StopTime is before StartTime")
		}
	}

	var resp OkReply
	if err := session.Exec(ctx, rpc, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("create-subscription: operation failed, <ok> not received")
	}
	return nil
}
