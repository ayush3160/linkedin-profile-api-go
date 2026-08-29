package linkedin

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Object is a JSON object that remembers key order.
//
// Order matters here. SDUI components render content out of several named
// props at once -- a layout element carries renderedToolbar and
// rendererWorkspace side by side -- and the order those appear in is the order
// they appear on the page. Decoding into a Go map would randomise it and
// scramble the extracted text, so the parser cannot use one.
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject returns an empty ordered object.
func NewObject() *Object {
	return &Object{vals: make(map[string]any, 8)}
}

// Set appends a key (or overwrites in place if it already exists).
func (o *Object) Set(key string, value any) {
	if _, exists := o.vals[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = value
}

// Get returns the value for key.
func (o *Object) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[key]
	return v, ok
}

// Str returns a string value, or "" if absent or another type.
func (o *Object) Str(key string) string {
	v, _ := o.Get(key)
	s, _ := v.(string)
	return s
}

// Has reports whether key is present.
func (o *Object) Has(key string) bool {
	_, ok := o.Get(key)
	return ok
}

// Len returns the number of keys.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Each calls fn for every key in document order.
func (o *Object) Each(fn func(key string, value any)) {
	if o == nil {
		return
	}
	for _, k := range o.keys {
		fn(k, o.vals[k])
	}
}

// decodeOrdered parses JSON preserving object key order. Objects become
// *Object, arrays []any, numbers json.Number, and the rest their Go natives.
func decodeOrdered(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := tok.(json.Delim)
	if !isDelim {
		return tok, nil
	}
	switch delim {
	case '{':
		obj := NewObject()
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, fmt.Errorf("object key was %T, not string", keyTok)
			}
			value, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			obj.Set(key, value)
		}
		if _, err := dec.Token(); err != nil { // closing }
			return nil, err
		}
		return obj, nil
	case '[':
		arr := make([]any, 0, 4)
		for dec.More() {
			value, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, value)
		}
		if _, err := dec.Token(); err != nil { // closing ]
			return nil, err
		}
		return arr, nil
	}
	return nil, fmt.Errorf("unexpected delimiter %v", delim)
}

// MarshalJSON writes the object back out with its keys in their original
// order.
//
// Object keeps its keys and values in unexported fields, so without this the
// encoder finds nothing to write and emits "{}". That is silent and total: a
// replayed card request would carry "payload":{} with no vanityName, and
// LinkedIn answers every one of them with HTTP 500. Round-tripping a
// discovered request argument is the whole point of discover-then-replay, so
// this method is load-bearing, not a convenience.
func (o *Object) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		encoded, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(encoded)
		buf.WriteByte(':')
		if encoded, err = json.Marshal(o.vals[key]); err != nil {
			return nil, err
		}
		buf.Write(encoded)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
