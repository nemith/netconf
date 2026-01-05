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

// RPCOption represents an option that can be passed to NewRPC to customize the
// created RPC message.
type RPCOption interface {
	apply(*RPC)
}

type attributesOpt []xml.Attr

func (o attributesOpt) apply(rpc *RPC) {
	rpc.Attributes = append(rpc.Attributes, o...)
}

// WithAttributes adds the given XML attributes to be added to the <rpc>
// element.  NETCONF servers are required to reflect these attributes in the
// associated <rpc-reply>.
func WithAttributes(attrs ...xml.Attr) RPCOption { return attributesOpt(attrs) }

// NewRPC creates a new RPC message with the given operation as the body.
func NewRPC(op any, opts ...RPCOption) *RPC {
	rpc := &RPC{
		Operation: op,
	}
	for _, opt := range opts {
		opt.apply(rpc)
	}
	return rpc
}

// Clone does a shallow copy of the RPC.  Operation is not deeply copied.
func (r *RPC) Clone() *RPC {
	return &RPC{
		MessageID:  r.MessageID,
		Attributes: slices.Clone(r.Attributes),
		Operation:  r.Operation,
	}
}

// RPCReply maps the xml value of <rpc-reply> in RFC6241.
type RPCReply struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc-reply"`

	// The message-id must match that of the associated <rpc>
	MessageID string `xml:"message-id,attr"`

	// Additional attributes on the <rpc-reply>.
	Attributes []xml.Attr `xml:",attr"`

	RPCErrors RPCErrors `xml:"rpc-error,omitempty"`
}

// Error types, severities, and tags as defined in RFC6241 sec 6.4.1
type ErrSeverity string

const (
	SevError   ErrSeverity = "error"
	SevWarning ErrSeverity = "warning"
)

// ErrType represents the error-type of a NETCONF RPC error.
type ErrType string

const (
	ErrTypeTransport ErrType = "transport"
	ErrTypeRPC       ErrType = "rpc"
	ErrTypeProtocol  ErrType = "protocol"
	ErrTypeApp       ErrType = "app"
)

// ErrTag represents the error-tag of a NETCONF RPC error.
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

// RPCError maps the xml value of <rpc-error> in RFC6241 sec 6.4.1.
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

// RPCErrors represents a list of RPCError returned from a NETCONF rpc-reply.
type RPCErrors []RPCError

// Filter returns a new RPCErrors slice containing only errors with the given
// severity.
func (errs RPCErrors) Filter(severity ErrSeverity) RPCErrors {
	if len(errs) == 0 {
		return nil
	}

	filteredErrs := make(RPCErrors, 0, len(errs))
	for _, err := range errs {
		if err.Severity != severity {
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

// Notification maps the xml value of <notification> in RFC5277
type Notification struct {
	XMLName   xml.Name  `xml:"urn:ietf:params:xml:ns:netconf:notification:1.0 notification"`
	EventTime time.Time `xml:"eventTime"`
}

// Hello maps the xml value of the <hello> message in RFC6241 sec 3.3.
type Hello struct {
	XMLName      xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 hello"`
	SessionID    uint64   `xml:"session-id,omitempty"`
	Capabilities []string `xml:"capabilities>capability"`
}

// Message represents a generic NETCONF stream message.  This could either be a
// response to a rpc invokcation (with the envelope og <rpc-reply>) or a
// notification message (with a envelope of <notification>).
//
// Messages MUST BE closed (with a call to Close(), or callind Decode() with
// will close for you) when no longer needed to release the session to process
// new messages.
type Message struct {
	// Additional XML attributes from the envelope (rpc-reply or notification)
	Attributes []xml.Attr // Any other attributes on the envelope

	reader    io.ReadCloser
	messageID *string // cached message-id attribute
}

// MessageID returns the message-id attribute from the rpc-reply envelope.  If
// the message doesn't have a message-id attribute (for example it is a
// notification), an empty string is returned.
func (d *Message) MessageID() string {
	// check for a cached value first
	if d.messageID != nil {
		return *d.messageID
	}

	for _, attr := range d.Attributes {
		if attr.Name.Local == "message-id" {
			d.messageID = &attr.Value
			return attr.Value
		}
	}

	return ""
}

func (d *Message) Read(p []byte) (n int, err error) {
	if d.reader == nil {
		return 0, io.EOF
	}
	return d.reader.Read(p)
}

func (d *Message) Close() error {
	if d.reader == nil {
		return nil
	}
	return d.reader.Close()
}

// Decode will decode the response XML into the provided value v and then close
// the message releasing the session to process new messages.
func (d *Message) Decode(v any) (err error) {
	defer func() {
		err = errors.Join(err, d.Close())
	}()

	if err := xml.NewDecoder(d).Decode(v); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return err
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
