package rpc

import (
	"encoding/xml"
	"maps"
	"slices"
)

type Filter interface {
	xml.Marshaler
	filter()
}

type subtreeFilter struct {
	f any
}

func (f subtreeFilter) filter() {}

func (f subtreeFilter) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "type"}, Value: "subtree"})

	switch v := f.f.(type) {
	case string:
		inner := struct {
			Data string `xml:",innerxml"`
		}{Data: v}
		return e.EncodeElement(&inner, start)
	case []byte:
		inner := struct {
			Data []byte `xml:",innerxml"`
		}{Data: v}
		return e.EncodeElement(&inner, start)
	default:
		return e.EncodeElement(f.f, start)

	}
}

// SubtreeFilter creates a filter matching the provided XML structure(s).
// Multiple arguments are merged into a single filter element as siblings.
func SubtreeFilter(filter any) Filter {
	return subtreeFilter{f: filter}
}

type xpathFilter struct {
	Select     string
	Namespaces map[string]string
}

func (f xpathFilter) filter() {}

func (f xpathFilter) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	start.Attr = append(start.Attr,
		xml.Attr{Name: xml.Name{Local: "type"}, Value: "xpath"},
		xml.Attr{Name: xml.Name{Local: "select"}, Value: f.Select},
	)

	for _, prefix := range slices.Sorted(maps.Keys(f.Namespaces)) {
		uri := f.Namespaces[prefix]
		attrName := "xmlns:" + prefix
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: attrName}, Value: uri})
	}

	return e.EncodeElement(struct{}{}, start)
}

// XPathFilter creates a filter using XPath 1.0 expression.
// namespaces map prefixes used in the path to their URIs.
func XPathFilter(path string, namespaces map[string]string) Filter {
	return xpathFilter{Select: path, Namespaces: namespaces}
}
