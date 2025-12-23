package rpc

//// CreateSubscriptionOption is a optional arguments to [Session.CreateSubscription] method
//type CreateSubscriptionOption interface {
//	apply(req *CreateSubscriptionReq)
//}
//
//type CreateSubscriptionReq struct {
//	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:netconf:notification:1.0 create-subscription"`
//	Stream  string   `xml:"stream,omitempty"`
//	// TODO: Implement filter
//	//Filter    int64    `xml:"filter,omitempty"`
//	StartTime string `xml:"startTime,omitempty"`
//	EndTime   string `xml:"endTime,omitempty"`
//}
//
//type stream string
//type startTime time.Time
//type endTime time.Time
//
//func (o stream) apply(req *CreateSubscriptionReq) {
//	req.Stream = string(o)
//}
//func (o startTime) apply(req *CreateSubscriptionReq) {
//	req.StartTime = time.Time(o).Format(time.RFC3339)
//}
//func (o endTime) apply(req *CreateSubscriptionReq) {
//	req.EndTime = time.Time(o).Format(time.RFC3339)
//}
//
//func WithStreamOption(s string) CreateSubscriptionOption        { return stream(s) }
//func WithStartTimeOption(st time.Time) CreateSubscriptionOption { return startTime(st) }
//func WithEndTimeOption(et time.Time) CreateSubscriptionOption   { return endTime(et) }
//
//func (s *Session) CreateSubscription(ctx context.Context, opts ...CreateSubscriptionOption) error {
//	var req CreateSubscriptionReq
//	for _, opt := range opts {
//		opt.apply(&req)
//	}
//	// TODO: eventual custom notifications rpc logic, e.g. create subscription only if notification capability is present
//
//	var resp OkReply
//	return s.Call(ctx, &req, &resp)
//}
