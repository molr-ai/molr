package tools

import (
	"encoding/json"
	"testing"
)

func TestTransferRequiresExplicitRemoteAndVersionedOverwrite(t *testing.T) {
	good := `{"source":{"path":"/work/a"},"destination":{"sandbox_id":"remote","resource_id":"directory","path":"/a"},"overwrite":false}`
	if _, err := parseFileTransfer(json.RawMessage(good)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"source":{"path":"/work/a"},"destination":{"path":"/b"},"overwrite":false}`,
		`{"source":{"sandbox_id":"a","path":"/a"},"destination":{"sandbox_id":"b","path":"/b"},"overwrite":false}`,
		`{"source":{"path":"/work/a"},"destination":{"sandbox_id":"remote","resource_id":"directory","path":"/a"},"overwrite":true}`,
		`{"source":{"path":"/work/a","owner_id":"other"},"destination":{"sandbox_id":"remote","path":"/a"},"overwrite":false}`,
	} {
		if _, err := parseFileTransfer(json.RawMessage(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
}
