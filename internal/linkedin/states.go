package linkedin

import (
	"regexp"
	"sort"
	"strings"
)

// This file reads SDUI model state out of a Flight document.
//
// The server ships the client's initial state alongside the UI. Two entries
// are worth mining because they are unambiguous where the rendered text is
// not:
//
//	profile_network_distance_<memberId>                    = "Distance2"
//	urn:li:fsd_followingState:urn:li:member:<numericId>    = "Follow"
//
// The first is the only reliable source of both the ACoAAA... member id and
// the viewer's network distance. The top card renders every degree variant
// ("· 1st", "· 2nd", "· 3rd") because it also ships the UI for states the
// viewer is not currently in, so the rendered text cannot settle it.

var (
	distanceKey   = regexp.MustCompile(`^profile_network_distance_(.+)$`)
	distanceValue = regexp.MustCompile(`^Distance(\d)$`)
	memberURN     = regexp.MustCompile(`^urn:li:fsd_followingState:urn:li:member:(\d+)$`)
)

var ordinals = map[string]string{"1": "1st", "2": "2nd", "3": "3rd"}

// ModelStates collects {stateID: value} from every SDUI state entry.
func ModelStates(doc *Document) map[string]string {
	states := make(map[string]string, 64)
	var descend func(node any, depth int)
	descend = func(node any, depth int) {
		if depth > maxDepth {
			return
		}
		switch value := node.(type) {
		case *Object:
			collectState(value, states)
			value.Each(func(_ string, child any) { descend(child, depth+1) })
		case []any:
			for _, child := range value {
				descend(child, depth+1)
			}
		}
	}
	ids := make([]string, 0, len(doc.Rows))
	for id, row := range doc.Rows {
		if row.Kind == RowModel {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		descend(doc.Rows[id].Value, 0)
	}
	return states
}

func collectState(node *Object, states map[string]string) {
	keyValue, hasKey := node.Get("key")
	valueValue, hasValue := node.Get("value")
	if !hasKey || !hasValue {
		return
	}
	keyObj, ok := keyValue.(*Object)
	if !ok {
		return
	}
	valueObj, ok := valueValue.(*Object)
	if !ok {
		return
	}
	// The id is double-nested: {"key":{"key":{"value":{"id":...}}}}.
	inner := keyObj
	if nested, ok := keyObj.Get("key"); ok {
		if obj, ok := nested.(*Object); ok {
			inner = obj
		}
	}
	valueField, ok := inner.Get("value")
	if !ok {
		return
	}
	valueFieldObj, ok := valueField.(*Object)
	if !ok {
		return
	}
	stateID := valueFieldObj.Str("id")
	if stateID == "" {
		return
	}
	if _, exists := states[stateID]; exists {
		return
	}
	for _, field := range [...]string{"stringValue", "booleanValue", "intValue"} {
		if raw, ok := valueObj.Get(field); ok {
			states[stateID] = stringify(raw)
			return
		}
	}
}

func stringify(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(toString(v))
	}
}

// Identity is who the profile belongs to and how the viewer relates to them.
type Identity struct {
	MemberID        string
	NumericMemberID string
	NetworkDistance string
}

// ProfileIdentity extracts the member id and network distance from state.
func ProfileIdentity(states map[string]string) Identity {
	var identity Identity
	keys := make([]string, 0, len(states))
	for key := range states {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if match := distanceKey.FindStringSubmatch(key); match != nil {
			identity.MemberID = match[1]
			if digits := distanceValue.FindStringSubmatch(states[key]); digits != nil {
				identity.NetworkDistance = ordinals[digits[1]]
				if identity.NetworkDistance == "" {
					identity.NetworkDistance = "3rd+"
				}
			}
			break
		}
	}
	for _, key := range keys {
		if match := memberURN.FindStringSubmatch(key); match != nil {
			identity.NumericMemberID = match[1]
			break
		}
	}
	return identity
}
