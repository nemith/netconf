package rpc

import (
	"context"
	"encoding/xml"
	"fmt"

	"nemith.io/netconf"
)

// KillSessionReq represents the `<kill-session>` operation defined in
// [RFC6241 7.6] for terminating a NETCONF session.
//
// [RFC6241 7.6]: https://www.rfc-editor.org/rfc/rfc6241.html#section-7.6
type KillSession struct {
	SessionID uint
}

func (rpc *KillSession) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	req := struct {
		XMLName   xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 kill-session"`
		SessionID uint     `xml:"session-id"`
	}{
		SessionID: rpc.SessionID,
	}
	return e.EncodeElement(&req, start)
}

func (rpc *KillSession) Exec(ctx context.Context, session *netconf.Session) error {
	var resp OkReply
	if err := session.Exec(ctx, rpc, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("kill-session: operation failed, <ok> not received")
	}
	return nil
}
