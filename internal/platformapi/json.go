package platformapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

func decodeNested(w http.ResponseWriter, r *http.Request, target any) bool {
	return decodeJSON(w, r, target, true)
}

// The role policy request is the only nested input. Reject duplicate keys at
// every level and bound structural depth/work before typed schema decoding.
func strictNestedObject(body []byte) bool {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	budget := 256
	var value func(int) bool
	value = func(depth int) bool {
		budget--
		if budget < 0 || depth > 6 {
			return false
		}
		t, err := d.Token()
		if err != nil {
			return false
		}
		delim, container := t.(json.Delim)
		if !container {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' || !value(0) {
		return false
	}
	var extra any
	return d.Decode(&extra) == io.EOF
}
