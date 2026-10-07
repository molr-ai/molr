package tools

import (
	"encoding/json"
	"testing"
)

func TestProfileArguments(t *testing.T) {
	for _, raw := range []string{
		`{"avatar":{"type":"file","path":"/tmp/avatar.png"},"expected_version":1}`,
		`{"name":"小满","expected_version":1}`,
		`{"avatar":{"type":"preset","id":"cat"},"expected_version":1}`,
		`{"avatar":{"type":"clear"},"expected_version":1}`,
		`{"avatar":{"type":"file","path":"/work/avatar.png","sandbox_id":"local"},"expected_version":1}`,
		`{"avatar":{"type":"file","path":"/resources/storage/avatar.png"},"expected_version":1}`,
	} {
		if _, err := parseProfile(json.RawMessage(raw)); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{}`, `null`, `{"name":"A"}`, `{"expected_version":1}`, `{"name":null,"expected_version":1}`,
		`{"name":"A","expected_version":0}`, `{"name":"A","expected_version":-1}`,
		`{"name":"A","expected_version":1,"assistant_id":"other"}`,
		`{"name":"A","expected_version":1,"budget":0}`,
		`{"avatar":{"type":"preset","id":"missing"},"expected_version":1}`,
		`{"avatar":{"type":"clear","id":"cat"},"expected_version":1}`,
		`{"avatar":{"type":"preset","id":"cat","path":""},"expected_version":1}`,
		`{"avatar":{"type":"file","path":null},"expected_version":1}`,
		`{"avatar":{"type":"file","path":"../other/a.png"},"expected_version":1}`,
		`{"avatar":{"type":"file","path":"https://example.com/a.png"},"expected_version":1}`,
		`{"avatar":{"type":"file","path":"/resources/a/x","resource_id":"b"},"expected_version":1}`,
		`{"name":"A","expected_version":1} {}`,
	} {
		if _, err := parseProfile(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}
