package rpc

import (
	"context"
	"encoding/xml"

	"nemith.io/netconf"
)

type ExtantBool bool

func (b ExtantBool) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if !b {
		return nil
	}
	// This produces a empty start/end tag (i.e <tag></tag>) vs a self-closing
	// tag (<tag/>) which should be the same in XML, however I know certain
	// vendors may have issues with this format. We may have to process this
	// after xml encoding.
	//
	// See https://github.com/golang/go/issues/21399
	// or https://github.com/golang/go/issues/26756 for a different hack.
	return e.EncodeElement(struct{}{}, start)
}

func (b *ExtantBool) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	*b = true
	return d.Skip()
}

type OkReply struct {
	netconf.RPCReply
	OK ExtantBool `xml:"ok"`
}

type Get struct {
	Filter Filter `xml:"filter,omitempty"`
}

func (rpc *Get) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	req := struct {
		XMLName xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 get"`
		Filter  Filter   `xml:"filter,omitempty"`
	}{
		Filter: rpc.Filter,
	}
	return e.Encode(&req)
}

type GetReply struct {
	netconf.RPCReply
	Data struct {
		XML []byte `xml:",innerxml"`
	} `xml:"data"`
}

func (rpc *Get) Exec(ctx context.Context, session *netconf.Session) ([]byte, error) {
	var resp GetReply
	if err := session.Exec(ctx, rpc, &resp); err != nil {
		return nil, err
	}

	return resp.Data.XML, nil
}
