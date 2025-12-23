package netconf

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// RPC maps the xml value of <rpc> in RFC6241
type RPC struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc"`

	// Managed by the session.  Will be overwritten when sent on the wire.
	MessageID string `xml:"message-id,attr"`

	// User-defined attributes (e.g. xmlns:ex="...").  Per RFC6241 sec 7.3, these
	// must be preserved and reflected in the associated <rpc-reply>.
	Attributes []xml.Attr `xml:",attr"`

	// The inner XML of the RPC message (e.g. <get-config>, <edit-config>)
	Operation any `xml:",innerxml"` // The operation payload (e.g. <get-config>)
}

type RPCReply struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc-reply"`

	// The message-id must match that of the associated <rpc>
	MessageID string `xml:"message-id,attr"`

	// Additional attributes on the <rpc-reply>.
	Attributes []xml.Attr `xml:",attr"`

	RPCErrors RPCErrors `xml:"rpc-error,omitempty"`
}

type ErrSeverity string

const (
	SevError   ErrSeverity = "error"
	SevWarning ErrSeverity = "warning"
)

type ErrType string

const (
	ErrTypeTransport ErrType = "transport"
	ErrTypeRPC       ErrType = "rpc"
	ErrTypeProtocol  ErrType = "protocol"
	ErrTypeApp       ErrType = "app"
)

type ErrTag string

const (
	ErrInUse                 ErrTag = "in-use"
	ErrInvalidValue          ErrTag = "invalid-value"
	ErrTooBig                ErrTag = "too-big"
	ErrMissingAttribute      ErrTag = "missing-attribute"
	ErrBadAttribute          ErrTag = "bad-attribute"
	ErrUnknownAttribute      ErrTag = "unknown-attribute"
	ErrMissingElement        ErrTag = "missing-element"
	ErrBadElement            ErrTag = "bad-element"
	ErrUnknownElement        ErrTag = "unknown-element"
	ErrUnknownNamespace      ErrTag = "unknown-namespace"
	ErrAccesDenied           ErrTag = "access-denied"
	ErrLockDenied            ErrTag = "lock-denied"
	ErrResourceDenied        ErrTag = "resource-denied"
	ErrRollbackFailed        ErrTag = "rollback-failed"
	ErrDataExists            ErrTag = "data-exists"
	ErrDataMissing           ErrTag = "data-missing"
	ErrOperationNotSupported ErrTag = "operation-not-supported"
	ErrOperationFailed       ErrTag = "operation-failed"
	ErrPartialOperation      ErrTag = "partial-operation"
	ErrMalformedMessage      ErrTag = "malformed-message"
)

type RPCError struct {
	Type     ErrType     `xml:"error-type"`
	Tag      ErrTag      `xml:"error-tag"`
	Severity ErrSeverity `xml:"error-severity"`
	AppTag   string      `xml:"error-app-tag,omitempty"`
	Path     string      `xml:"error-path,omitempty"`
	Message  string      `xml:"error-message,omitempty"`
	Info     RawXML      `xml:"error-info,omitempty"`
}

func (e RPCError) Error() string {
	return fmt.Sprintf("netconf error: %s %s: %s", e.Type, e.Tag, e.Message)
}

type RPCErrors []RPCError

func (errs RPCErrors) Filter(severity ...ErrSeverity) RPCErrors {
	if len(errs) == 0 {
		return nil
	}

	if len(severity) == 0 {
		severity = []ErrSeverity{SevError}
	}

	filteredErrs := make(RPCErrors, 0, len(errs))
	for _, err := range errs {
		if !slices.Contains(severity, err.Severity) {
			continue
		}
		filteredErrs = append(filteredErrs, err)
	}
	return filteredErrs
}

func (errs RPCErrors) Error() string {
	if len(errs) == 0 {
		return ""
	}

	if len(errs) == 1 {
		return errs[0].Error()
	}

	var sb strings.Builder
	sb.WriteString("multiple netconf errors:\n")
	for i, err := range errs {
		if i > 0 {
			sb.WriteRune('\n')
		}
		sb.WriteString(err.Error())
	}
	return sb.String()
}

func (errs RPCErrors) Unwrap() error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	unboxedErrs := make([]error, len(errs))
	for i, err := range errs {
		unboxedErrs[i] = err
	}
	return errors.Join(unboxedErrs...)
}

type Notification struct {
	XMLName   xml.Name  `xml:"urn:ietf:params:xml:ns:netconf:notification:1.0 notification"`
	EventTime time.Time `xml:"eventTime"`
}

// HelloMsg maps the xml value of the <hello> message in RFC6241
type HelloMsg struct {
	XMLName      xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 hello"`
	SessionID    uint64   `xml:"session-id,omitempty"`
	Capabilities []string `xml:"capabilities>capability"`
}

type Request struct {
	RPC RPC
}

func NewRequest(op any) *Request {
	return &Request{
		RPC: RPC{
			Operation: op,
		},
	}
}

type Response struct {
	io.ReadCloser

	MessageID  string     // Captured from the message-id attribute
	Attributes []xml.Attr // Any other attributes on the envelope
}

// Decode will decode the response XML into the provided value v and then close
// the message releasing the session to process new messages.
func (d *Response) Decode(v any) (err error) {
	defer func() {
		err = errors.Join(err, d.Close())
	}()

	if err := xml.NewDecoder(d).Decode(v); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return err
}

func (d *Response) Close() error {
	if d.ReadCloser == nil {
		return nil
	}
	return d.ReadCloser.Close()
}

// RawXML is a helper type for getting innerxml content as a byte slice.
type RawXML []byte

func (x *RawXML) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var inner struct {
		Data []byte `xml:",innerxml"`
	}

	if err := d.DecodeElement(&inner, &start); err != nil {
		return err
	}

	*x = inner.Data
	return nil
}

func (x RawXML) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	inner := struct {
		Data []byte `xml:",innerxml"`
	}{
		Data: []byte(x),
	}
	return e.EncodeElement(&inner, start)
}
