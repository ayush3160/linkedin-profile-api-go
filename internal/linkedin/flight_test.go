package linkedin

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDocumentHandlesEveryRowKind(t *testing.T) {
	stream := strings.Join([]string{
		`1:I["abc",[],"Text"]`,
		`2:["$","div",null,{"children":"hi"}]`,
		`3:"a string"`,
		`4:null`,
	}, "\n")

	doc, err := ParseDocument(stream)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if got := doc.Rows["1"].Kind; got != RowClient {
		t.Errorf("row 1 kind = %v, want RowClient", got)
	}
	if !IsElement(doc.Rows["2"].Value) {
		t.Error("row 2 should be a React element")
	}
	if got, _ := doc.Rows["3"].Value.(string); got != "a string" {
		t.Errorf("row 3 = %q", got)
	}
	if doc.Rows["4"].Value != nil {
		t.Error("row 4 should be nil")
	}
}

func TestParseDocumentSkipsMalformedLines(t *testing.T) {
	doc, err := ParseDocument("1:{\"a\":1}\nnot a row\n2:{\"b\":\n3:\"ok\"")
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if len(doc.Rows) != 2 {
		t.Fatalf("got %d rows, want 2 (%v)", len(doc.Rows), doc.Rows)
	}
	for _, id := range []string{"1", "3"} {
		if _, ok := doc.Rows[id]; !ok {
			t.Errorf("row %s missing", id)
		}
	}
}

func TestParseDocumentRejectsEmptyInput(t *testing.T) {
	if _, err := ParseDocument(""); err != ErrNotFlight {
		t.Errorf("err = %v, want ErrNotFlight", err)
	}
}

func TestDerefResolvesAndStopsAtCycles(t *testing.T) {
	doc, err := ParseDocument("0:[\"$L1\"]\n1:[\"$L0\"]")
	if err != nil {
		t.Fatal(err)
	}
	value, id, ok := doc.Deref("$L1", map[string]bool{})
	if !ok || id != "1" {
		t.Fatalf("Deref($L1) = %v, %q, %v", value, id, ok)
	}
	if _, _, ok := doc.Deref("$L1", map[string]bool{"1": true}); ok {
		t.Error("already-expanded row should not resolve again on the same path")
	}
	if _, _, ok := doc.Deref("$L9", map[string]bool{}); ok {
		t.Error("unknown row should not resolve")
	}
	if _, _, ok := doc.Deref("not-a-ref", map[string]bool{}); ok {
		t.Error("non-reference should not resolve")
	}
}

func TestDerefReturnsClientExportName(t *testing.T) {
	doc, err := ParseDocument("0:null\n1:I[\"hash\",[],\"ReplaceableComponent\"]")
	if err != nil {
		t.Fatal(err)
	}
	value, _, ok := doc.Deref("$L1", map[string]bool{})
	if !ok {
		t.Fatal("client row should resolve")
	}
	ref, ok := value.(clientRef)
	if !ok || ref.Export != "ReplaceableComponent" {
		t.Errorf("got %#v", value)
	}
}

// Object key order drives document order in multi-slot components, so it must
// survive decoding.
func TestDecodeOrderedPreservesKeyOrder(t *testing.T) {
	value, err := decodeOrdered([]byte(`{"zebra":1,"apple":2,"middle":3}`))
	if err != nil {
		t.Fatal(err)
	}
	obj, ok := value.(*Object)
	if !ok {
		t.Fatalf("got %T", value)
	}
	var keys []string
	obj.Each(func(key string, _ any) { keys = append(keys, key) })
	want := []string{"zebra", "apple", "middle"}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("key order = %v, want %v", keys, want)
		}
	}
}

// A discovered request argument has to survive a decode/encode round trip. It
// did not: Object keeps its keys unexported and had no MarshalJSON, so the
// encoder emitted "{}" and every replayed card request went out with an empty
// payload. LinkedIn answered all of them with HTTP 500, and the API still
// returned 200 with empty sections -- a silent, total failure.
func TestObjectRoundTripsThroughJSON(t *testing.T) {
	const source = `{"$type":"proto.sdui.actions.requests.RequestedArguments",` +
		`"payload":{"isSelfView":false,"vanityName":"ada-lovelace"},` +
		`"requestMetadata":{"$type":"proto.sdui.common.RequestMetadata"}}`

	decoded, err := decodeOrdered([]byte(source))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(encoded); got != source {
		t.Fatalf("round trip changed the payload:\n got: %s\nwant: %s", got, source)
	}
	if !bytes.Contains(encoded, []byte(`"vanityName":"ada-lovelace"`)) {
		t.Error("vanityName was dropped -- LinkedIn answers such a request with HTTP 500")
	}
}
