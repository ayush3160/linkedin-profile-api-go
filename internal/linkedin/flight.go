// Package linkedin speaks LinkedIn flagship-web's React Server Components
// transport and reconstructs a profile from the UI tree it carries.
package linkedin

import (
	"errors"
	"strings"
)

// ErrNotFlight is returned when a body contains no parseable Flight rows.
var ErrNotFlight = errors.New("no Flight rows found (not an RSC response?)")

// RowKind distinguishes a client-component reference from a model row.
type RowKind int

const (
	// RowModel is a JSON model or React element: ["$", type, key, props].
	RowModel RowKind = iota
	// RowClient is a client-component reference: [chunkHash, deps, exportName].
	RowClient
)

// Row is one decoded line of a Flight stream.
type Row struct {
	Kind  RowKind
	Value any
}

// Document is a parsed Flight stream.
//
// The wire format is one row per line, "<hexid>:<payload>", with four payload
// shapes observed against a full capture of a profile visit (232/232 rows
// parsed, zero failures):
//
//	I[...]   client-component reference
//	[...]    model or React element
//	"..."    plain string
//	null     empty row
//
// Values may reference other rows: "$L4e" (lazy, flushed later in the same
// stream), "$4e" (direct), "$undefined", "$Sreact.fragment", "$n900" (BigInt)
// and "$cd:props:children:1:..." path aliases.
type Document struct {
	Rows map[string]Row
}

// ParseDocument decodes a Flight stream.
//
// Lines that do not match the row grammar are skipped rather than fatal: the
// stream is chunked and a truncated tail is normal.
func ParseDocument(text string) (*Document, error) {
	rows := make(map[string]Row, 256)
	for _, line := range strings.Split(text, "\n") {
		id, payload, ok := splitRow(line)
		if !ok {
			continue
		}
		kind := RowModel
		if strings.HasPrefix(payload, "I") {
			kind, payload = RowClient, payload[1:]
		}
		value, err := decodeOrdered([]byte(payload))
		if err != nil {
			continue
		}
		rows[id] = Row{Kind: kind, Value: value}
	}
	if len(rows) == 0 {
		return nil, ErrNotFlight
	}
	return &Document{Rows: rows}, nil
}

// splitRow matches "^([0-9a-f]+):(.*)$" without the regexp overhead; these
// streams run to millions of lines across a profile fetch.
func splitRow(line string) (id, payload string, ok bool) {
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return "", "", false
	}
	for i := 0; i < colon; i++ {
		c := line[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", "", false
		}
	}
	return line[:colon], line[colon+1:], true
}

// Root is row 0, the entry point of the render tree.
func (d *Document) Root() any {
	if row, ok := d.Rows["0"]; ok {
		return row.Value
	}
	return nil
}

// Deref resolves "$L<id>" / "$<id>" to its row payload.
//
// seen holds the ids already expanded on the current path, which is what keeps
// the self-referential rows LinkedIn emits from recursing forever. Callers add
// the returned id to seen before descending and remove it afterwards, giving
// per-path rather than global cycle detection.
//
// A client row resolves to its export name, since the implementation lives in
// a JS chunk we never load.
func (d *Document) Deref(ref string, seen map[string]bool) (value any, id string, ok bool) {
	id, ok = hexRef(ref)
	if !ok || seen[id] {
		return nil, "", false
	}
	row, exists := d.Rows[id]
	if !exists {
		return nil, "", false
	}
	if row.Kind == RowClient {
		return clientRef{Export: exportName(row.Value)}, id, true
	}
	return row.Value, id, true
}

// clientRef stands in for a client component; only its export name is useful.
type clientRef struct{ Export string }

func exportName(value any) string {
	parts, ok := value.([]any)
	if !ok || len(parts) < 3 {
		return "?"
	}
	name, _ := parts[2].(string)
	if name == "" {
		return "?"
	}
	return name
}

// hexRef parses "$L4e" or "$4e" into "4e".
func hexRef(s string) (string, bool) {
	if len(s) < 2 || s[0] != '$' {
		return "", false
	}
	rest := s[1:]
	if rest[0] == 'L' {
		rest = rest[1:]
	}
	if rest == "" {
		return "", false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", false
		}
	}
	return rest, true
}

// IsElement reports whether node is a React element: ["$", type, key, props].
func IsElement(node any) bool {
	list, ok := node.([]any)
	if !ok || len(list) < 4 {
		return false
	}
	marker, ok := list[0].(string)
	return ok && marker == "$"
}
