package rpc

import (
	"context"
	"encoding/xml"
	"fmt"

	"nemith.io/netconf"
)

// Datastore represents a NETCONF configuration datastore as defined in
// RFC6241 section 7.1
type Datastore string

func (d Datastore) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if d == "" {
		return fmt.Errorf("datastore name cannot be empty")
	}

	for i := range len(d) {
		c := d[i]
		if (c < 'a' || c > 'z') &&
			(c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') &&
			c != '_' && c != '-' && c != '.' {
			return fmt.Errorf("invalid datastore name: %q", d)
		}
	}

	inner := struct {
		Elem string `xml:",innerxml"`
	}{Elem: "<" + string(d) + "/>"}

	// EncodeElement(nil, ...) creates a self-closing tag <running/>
	return e.EncodeElement(&inner, start)
}

const (
	// Running configuration datastore. Required by RFC6241
	Running Datastore = "running"

	// Candidate configuration configuration datastore.  Supported with the
	// `:candidate` capability defined in RFC6241 section 8.3
	Candidate Datastore = "candidate"

	// Startup configuration configuration datastore.  Supported with the
	// `:startup` capability defined in RFC6241 section 8.7
	Startup Datastore = "startup"
)

type URL string

func (u URL) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	v := struct {
		URL string `xml:"url"`
	}{string(u)}
	return e.EncodeElement(&v, start)
}

// GetConfig implements the <get-config> rpc operation defined in [RFC6241 7.1].
// `source` is the datastore to query.
//
// [RFC6241 7.1]: https://www.rfc-editor.org/rfc/rfc6241.html#section-7.1
type GetConfig struct {
	Source Datastore
	Filter Filter
}

func (op GetConfig) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	req := struct {
		XMLName xml.Name  `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 get-config"`
		Source  Datastore `xml:"source"`
		Filter  Filter    `xml:"filter,omitempty"`
	}{
		Source: op.Source,
		Filter: op.Filter,
	}

	return e.Encode(&req)
}

func (rpc GetConfig) Exec(ctx context.Context, session *netconf.Session) ([]byte, error) {
	var reply GetConfigReply
	if err := session.Exec(ctx, rpc, &reply); err != nil {
		return nil, err
	}

	return reply.Config, nil
}

type GetConfigReply struct {
	Config []byte
}

func (r *GetConfigReply) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type dataWrapper struct {
		Data struct {
			Inner []byte `xml:",innerxml"`
		} `xml:"data"`
	}

	var wrapper dataWrapper
	if err := d.DecodeElement(&wrapper, &start); err != nil {
		return err
	}
	r.Config = wrapper.Data.Inner
	return nil
}

// DefaultOperation defines the strategies for merging configuration in a
// `<edit-config> operation`.
type DefaultOperation string

const (
	// MergeConfig configuration elements are merged together at the level at
	// which this specified.  Can be used for config elements as well as default.
	MergeConfig DefaultOperation = "merge"

	// ReplaceConfig defines that the incoming config change should replace the
	// existing config at the level which it is specified.  This can be
	// specified on individual config elements or set as the default strategy.
	ReplaceConfig DefaultOperation = "replace"

	// NoneOperation indicates that no default operation should be applied and
	// nothing is applied to the target configuration unless there are
	// operations defined on the configs subelements.
	NoneOperation DefaultOperation = "none"
)

// TestOption defines the behavior for testing configuration before applying it
// in a `<edit-config>` operation.
type TestOption string

const (
	// TestThenSet will validate the configuration and only if is is valid then
	// apply the configuration to the datastore.
	TestThenSet TestOption = "test-then-set"

	// SetOnly will not do any testing before applying it.
	SetOnly TestOption = "set"

	// Test only will validatate the incoming configuration and return the
	// results without modifying the underlying store.
	TestOnly TestOption = "test-only"
)

// ErrorOption defines the behavior when an error is encountered during a
// `<edit-config>` operation.
type ErrorOption string

const (
	// StopOnError will abort the `<edit-config>` operation on the first error.
	StopOnError ErrorOption = "stop-on-error"

	// ContinueOnError will continue to parse the configuration data even if an
	// error is encountered.  Errors are still recorded and reported in the
	// reply.
	ContinueOnError ErrorOption = "continue-on-error"

	// RollbackOnError will restore the configuration back to before the
	// `<edit-config>` operation took place.  This requires the device to
	// support the `:rollback-on-error` capability.
	RollbackOnError ErrorOption = "rollback-on-error"
)

// EditConfig issues the `<edit-config>` operation defined in [RFC6241 7.2] for
// updating an existing target config datastore.
//
// [RFC6241 7.2]: https://www.rfc-editor.org/rfc/rfc6241.html#section-7.2
type EditConfig struct {
	Target           Datastore
	DefaultOperation DefaultOperation
	TestOption       TestOption
	ErrorOption      ErrorOption
	Config           any
}

func (rpc EditConfig) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	req := struct {
		XMLName          xml.Name         `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 edit-config"`
		Target           Datastore        `xml:"target"`
		DefaultOperation DefaultOperation `xml:"default-operation,omitempty"`
		TestOption       TestOption       `xml:"test-option,omitempty"`
		ErrorOption      ErrorOption      `xml:"error-option,omitempty"`

		Config any    `xml:"config,omitempty"`
		URL    string `xml:"url,omitempty"`
	}{
		Target:           rpc.Target,
		DefaultOperation: rpc.DefaultOperation,
		TestOption:       rpc.TestOption,
		ErrorOption:      rpc.ErrorOption,
	}

	switch v := rpc.Config.(type) {
	case URL:
		req.URL = string(v)
	case string:
		req.Config = struct {
			Inner string `xml:",innerxml"`
		}{Inner: v}
	case []byte:
		req.Config = struct {
			Inner []byte `xml:",innerxml"`
		}{Inner: v}
	default:
		req.Config = rpc.Config
	}

	return e.Encode(&req)
}

func (rpc EditConfig) Exec(ctx context.Context, session *netconf.Session) error {
	var resp OkReply
	if err := session.Exec(ctx, rpc, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("edit-config: operation failed, <ok> not received")
	}
	return nil
}

// CopyConfig issues the `<copy-config>` operation as defined in [RFC6241 7.3]
// for copying an entire config to/from a source and target datastore.
//
// A `<config>` element defining a full config can be used as the source.
//
// If a device supports the `:url` capability than a [URL] object can be used
// for the source or target datastore.
//
// [RFC6241 7.3] https://www.rfc-editor.org/rfc/rfc6241.html#section-7.3
type CopyConfig struct {
	Source any
	Target any
}

func (rpc CopyConfig) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	req := struct {
		XMLName xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 copy-config"`
		Source  any      `xml:"source"`
		Target  any      `xml:"target"`
	}{
		Source: rpc.Source,
		Target: rpc.Target,
	}

	return e.Encode(&req)
}

func (rpc CopyConfig) Exec(ctx context.Context, session *netconf.Session) error {
	var resp OkReply
	if err := session.Exec(ctx, rpc, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("copy-config: operation failed, <ok> not received")
	}

	return nil
}

// DeleteConfigReq represents the `<delete-config>` operation defined in
// [RFC6241 7.4] for deleting a configuration datastore.
//
// [RFC6241 7.4]: https://www.rfc-editor.org/rfc/rfc6241.html#section-7.4
type DeleteConfig struct {
	Target Datastore
}

func (rpc DeleteConfig) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	req := struct {
		XMLName xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 delete-config"`
		Target  any      `xml:"target"`
	}{
		Target: rpc.Target,
	}

	return e.Encode(&req)
}

func (rpc DeleteConfig) Exec(ctx context.Context, session *netconf.Session) error {
	var resp OkReply
	if err := session.Exec(ctx, rpc, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("delete-config: operation failed, <ok> not received")
	}

	return nil
}

// LockReq represents the `<lock>` operation defined in [RFC6241 7.5] for
// locking a configuration datastore.
//
// [RFC6241 7.5]: https://www.rfc-editor.org/rfc/rfc6241.html#section-7.5
type Lock struct {
	Target Datastore
}

func (rpc Lock) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	req := struct {
		XMLName xml.Name  `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 lock"`
		Target  Datastore `xml:"target"`
	}{
		Target: rpc.Target,
	}

	return e.Encode(&req)
}

func (rpc Lock) Exec(ctx context.Context, session *netconf.Session) error {
	var resp OkReply
	if err := session.Exec(ctx, rpc, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("lock: operation failed, <ok> not received")
	}

	return nil
}

type Unlock struct {
	Target Datastore
}

func (rpc Unlock) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	req := struct {
		XMLName xml.Name  `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 unlock"`
		Target  Datastore `xml:"target"`
	}{
		Target: rpc.Target,
	}

	return e.Encode(&req)
}

func (rpc Unlock) Exec(ctx context.Context, session *netconf.Session) error {
	var resp OkReply
	if err := session.Exec(ctx, rpc, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("unlock: operation failed, <ok> not received")
	}

	return nil
}

type Validate struct {
	Source any
}

func (rpc Validate) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	req := struct {
		XMLName xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 validate"`
		Source  any      `xml:"source"`
	}{
		Source: rpc.Source,
	}

	return e.Encode(&req)
}

func (rpc Validate) Exec(ctx context.Context, session *netconf.Session) error {
	var resp OkReply
	if err := session.Exec(ctx, rpc, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("validate: operation failed, <ok> not received")
	}

	return nil
}

// Commit represents the `<commit>` operation defined in [RFC6241 8.5] for
// committing candidate configuration to the running datastore.
//
// [RFC6241 8.5]: https://www.rfc-editor.org/rfc/rfc6241.html#section-8.5
type Commit struct {
	// Confirmed indicates that the commit must be confirmed with a follow-up
	// commit within the confirm-timeout period (default 600 seconds).  If not
	// confirmed, the commit will be reverted.
	//
	// Device must support :confirmed-commit:1.1 capability.
	Confirmed bool

	// ConfirmTimeout is the time in seconds to wait before reverting a
	// confirmed commit.
	//
	// Device must support :confirmed-commit:1.1 capability.
	ConfirmTimeout int64

	// Persist indicates that the confirmed commit can be persisted across
	// sessions and confirmed in a different session.
	//
	// If Confirmed is set this expands to the <persist> element.
	//
	// If Confirmed is not set this expands to the <persist-id> element to
	// confirm a previous commit with the same id.
	PersistID string
}

func (rpc Commit) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	req := struct {
		XMLName        xml.Name   `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 commit"`
		Confirmed      ExtantBool `xml:"confirmed,omitempty"`
		ConfirmTimeout int64      `xml:"confirm-timeout,omitempty"`
		Persist        string     `xml:"persist,omitempty"`
		PersistID      string     `xml:"persist-id,omitempty"`
	}{
		Confirmed:      ExtantBool(rpc.Confirmed),
		ConfirmTimeout: rpc.ConfirmTimeout,
	}

	if rpc.PersistID != "" {
		if rpc.Confirmed {
			req.Persist = rpc.PersistID
		} else {
			req.PersistID = rpc.PersistID
		}
	}

	return e.Encode(&req)
}

func (rpc Commit) Exec(ctx context.Context, session *netconf.Session) error {
	var resp OkReply
	if err := session.Exec(ctx, rpc, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("commit: operation failed, <ok> not received")
	}
	return nil
}

type CancelCommit struct {
	PersistID string
}

func (rpc CancelCommit) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	req := struct {
		XMLName   xml.Name `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 cancel-commit"`
		PersistID string   `xml:"persist-id,omitempty"`
	}{
		PersistID: rpc.PersistID,
	}
	return e.Encode(&req)
}

func (rpc CancelCommit) Exec(ctx context.Context, session *netconf.Session) error {
	var resp OkReply
	if err := session.Exec(ctx, rpc, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("cancel-commit: operation failed, <ok> not received")
	}
	return nil
}
