package netconf

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"golang.org/x/exp/slices"
)

var (
	RPCReplyName = xml.Name{
		Space: "urn:ietf:params:xml:ns:netconf:base:1.0",
		Local: "rpc-reply",
	}

	NofificationName = xml.Name{
		Space: "urn:ietf:params:xml:ns:netconf:notification:1.0",
		Local: "notification",
	}
)

// RawXML captures the raw xml for the given element.  Used to process certain
// elements later.
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

// MarshalXML implements xml.Marshaller.  Raw XML is passed verbatim, errors and
// all.
func (x *RawXML) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	inner := struct {
		Data []byte `xml:",innerxml"`
	}{
		Data: []byte(*x),
	}
	return e.EncodeElement(&inner, start)
}

// helloMsg maps the xml value of the <hello> message in RFC6241
type helloMsg struct {
	XMLName      xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 hello"`
	SessionID    uint64   `xml:"session-id,omitempty"`
	Capabilities []string `xml:"capabilities>capability"`
}

// request maps the xml value of <rpc> in RFC6241
type request struct {
	XMLName   xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc"`
	MessageID uint64   `xml:"message-id,attr"`
	Operation any      `xml:",innerxml"`
}

func (msg *request) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if msg.Operation == nil {
		return fmt.Errorf("operation cannot be nil")
	}

	// TODO: validate operation is named?

	// alias the type to not cause recursion calling e.Encode
	type rpcMsg request
	inner := rpcMsg(*msg)
	return e.Encode(&inner)
}

// Reply maps the xml value of <rpc-reply> in RFC6241
type Reply struct {
	XMLName   xml.Name  `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc-reply"`
	MessageID uint64    `xml:"message-id,attr"`
	Errors    RPCErrors `xml:"rpc-error,omitempty"`

	raw []byte `xml:"-"`
}

func ParseReply(data []byte) (*Reply, error) {
	reply := Reply{
		raw: data,
	}
	if err := xml.Unmarshal(data, &reply); err != nil {
		return nil, fmt.Errorf("couldn't parse reply: %v", err)
	}

	return &reply, nil
}

// Decode will decode the entire `rpc-reply` into a value pointed to by v.  This
// is a simple wrapper around xml.Unmarshal.
func (r Reply) Decode(v interface{}) error {
	if r.raw == nil {
		return fmt.Errorf("empty reply")
	}
	return xml.Unmarshal(r.raw, v)
}

// Raw returns the native message as it came from the server
func (r Reply) Raw() []byte {
	return r.raw
}

// String returns the message as string.
func (r Reply) String() string {
	return string(r.raw)
}

// Err will return go error(s) from a Reply that are of the given severities. If
// no severity is given then it defaults to `ErrSevError`.
//
// If one error is present then the underlyign type is `RPCError`. If more than
// one error exists than the underlying type is `[]RPCError`
//
// Example

// get all errors with severity of error
//
//	if err := reply.Err(ErrSevError); err != nil { /* ... */ }
//
// or
//
//	if err := reply.Err(); err != nil { /* ... */ }
//
// get all errors with severity of only warning
//
//	if err := reply.Err(ErrSevWarning); err != nil { /* ... */ }
//
// get all errors
//
//	if err := reply.Err(ErrSevWarning, ErrSevError); err != nil { /* ... */ }
func (r Reply) Err(severity ...ErrSeverity) error {
	// fast escape for no errors
	if len(r.Errors) == 0 {
		return nil
	}

	errs := r.Errors.Filter(severity...)
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return errs
	}
}

type Notification struct {
	XMLName   xml.Name  `xml:"urn:ietf:params:xml:ns:netconf:notification:1.0 notification"`
	EventTime time.Time `xml:"eventTime"`

	raw []byte `xml:"-"`
}

func ParseNotification(data []byte) (*Notification, error) {
	notif := Notification{
		raw: data,
	}
	if err := xml.Unmarshal(data, &notif); err != nil {
		return nil, fmt.Errorf("couldn't parse reply: %v", err)
	}

	return &notif, nil
}

// Decode will decode the entire `noticiation` into a value pointed to by v.
// This is a simple wrapper around xml.Unmarshal.
func (n Notification) Decode(v interface{}) error {
	if n.raw == nil {
		return fmt.Errorf("empty reply")
	}
	return xml.Unmarshal(n.raw, v)
}

// Raw returns the native message as it came from the server
func (n Notification) Raw() []byte {
	return n.raw
}

// String returns the message as string.
func (n Notification) String() string {
	return string(n.raw)
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
	var sb strings.Builder
	for i, err := range errs {
		if i > 0 {
			sb.WriteRune('\n')
		}
		sb.WriteString(err.Error())
	}
	return sb.String()
}

func (errs RPCErrors) Unwrap() []error {
	boxedErrs := make([]error, len(errs))
	for i, err := range errs {
		boxedErrs[i] = err
	}
	return boxedErrs
}
