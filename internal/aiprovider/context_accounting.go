package aiprovider

import (
	"fmt"
	"unicode/utf8"
)

// RequestContextComponentKind identifies one logical contribution to the
// final provider request. It is provider independent and contains no content.
type RequestContextComponentKind string

const (
	RequestContextGovernance           RequestContextComponentKind = "governance"
	RequestContextIntent               RequestContextComponentKind = "intent"
	RequestContextApprovedScope        RequestContextComponentKind = "approved_scope"
	RequestContextImpactReport         RequestContextComponentKind = "impact_report"
	RequestContextSource               RequestContextComponentKind = "source_context"
	RequestContextRepository           RequestContextComponentKind = "repository_context"
	RequestContextProviderInstructions RequestContextComponentKind = "provider_instructions"
	RequestContextOther                RequestContextComponentKind = "other"

	MaximumRequestContextComponents = 8
)

// RequestContextComponent is bounded numeric metadata about one serialized
// request contribution. It never retains the measured content.
type RequestContextComponent struct {
	kind           RequestContextComponentKind
	byteCount      int
	characterCount int
	itemCount      int
	omittedItems   int
	truncated      bool
}

// MeasureRequestContextComponent derives deterministic counts from the exact
// serialized contribution while retaining only bounded numeric metadata.
func MeasureRequestContextComponent(
	kind RequestContextComponentKind,
	serialized string,
	itemCount int,
	omittedItems int,
	truncated bool,
) (RequestContextComponent, error) {
	if !validRequestContextKind(kind) {
		return RequestContextComponent{}, fmt.Errorf("provider request context component kind %q is invalid", kind)
	}
	if itemCount < 0 || omittedItems < 0 {
		return RequestContextComponent{}, fmt.Errorf("provider request context component counts cannot be negative")
	}
	return RequestContextComponent{
		kind:           kind,
		byteCount:      len(serialized),
		characterCount: utf8.RuneCountInString(serialized),
		itemCount:      itemCount,
		omittedItems:   omittedItems,
		truncated:      truncated,
	}, nil
}

func (component RequestContextComponent) Kind() RequestContextComponentKind {
	return component.kind
}
func (component RequestContextComponent) ByteCount() int      { return component.byteCount }
func (component RequestContextComponent) CharacterCount() int { return component.characterCount }
func (component RequestContextComponent) ItemCount() int      { return component.itemCount }
func (component RequestContextComponent) OmittedItems() int   { return component.omittedItems }
func (component RequestContextComponent) Truncated() bool     { return component.truncated }

// RequestContextAccounting describes the exact serialized request without
// retaining prompt text, source bodies, provider streams, or credentials.
type RequestContextAccounting struct {
	components      []RequestContextComponent
	totalBytes      int
	totalCharacters int
	totalItems      int
	truncated       bool
}

// NewRequestContextAccounting validates and totals bounded component metadata.
func NewRequestContextAccounting(components []RequestContextComponent) (RequestContextAccounting, error) {
	if len(components) > MaximumRequestContextComponents {
		return RequestContextAccounting{}, fmt.Errorf(
			"provider request context accounting exceeds %d components",
			MaximumRequestContextComponents,
		)
	}
	seen := make(map[RequestContextComponentKind]struct{}, len(components))
	copyComponents := make([]RequestContextComponent, len(components))
	accounting := RequestContextAccounting{components: copyComponents}
	for index, component := range components {
		if !validRequestContextKind(component.kind) {
			return RequestContextAccounting{}, fmt.Errorf("provider request context component %d is invalid", index)
		}
		if _, exists := seen[component.kind]; exists {
			return RequestContextAccounting{}, fmt.Errorf("provider request context component %q is duplicated", component.kind)
		}
		seen[component.kind] = struct{}{}
		if component.byteCount < 0 || component.characterCount < 0 ||
			component.itemCount < 0 || component.omittedItems < 0 {
			return RequestContextAccounting{}, fmt.Errorf("provider request context component %q has invalid counts", component.kind)
		}
		copyComponents[index] = component
		accounting.totalBytes += component.byteCount
		accounting.totalCharacters += component.characterCount
		accounting.totalItems += component.itemCount
		accounting.truncated = accounting.truncated || component.truncated
	}
	return accounting, nil
}

func (accounting RequestContextAccounting) Components() []RequestContextComponent {
	return append([]RequestContextComponent(nil), accounting.components...)
}
func (accounting RequestContextAccounting) TotalBytes() int      { return accounting.totalBytes }
func (accounting RequestContextAccounting) TotalCharacters() int { return accounting.totalCharacters }
func (accounting RequestContextAccounting) TotalItems() int      { return accounting.totalItems }
func (accounting RequestContextAccounting) Truncated() bool      { return accounting.truncated }

func validRequestContextKind(kind RequestContextComponentKind) bool {
	switch kind {
	case RequestContextGovernance,
		RequestContextIntent,
		RequestContextApprovedScope,
		RequestContextImpactReport,
		RequestContextSource,
		RequestContextRepository,
		RequestContextProviderInstructions,
		RequestContextOther:
		return true
	default:
		return false
	}
}
