package installationwindows

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestWindowsTaskXMLFileUnicodeRoundTrip(t *testing.T) {
	// Include a surrogate pair as well as Chinese and XML escaping. Validate the
	// actual byte representation, then parse it after the same decoder as /Query.
	original := `<?xml version="1.0" encoding="UTF-8"?><Task><Command>C:\用户\测试 &amp; 🐕\molr.exe</Command></Task>`
	raw := taskXMLFile(original)
	if !bytes.HasPrefix(raw, []byte{0xff, 0xfe}) {
		t.Fatal("missing UTF-16LE BOM")
	}
	units := make([]uint16, (len(raw)-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[2+i*2:])
	}
	if !strings.Contains(string(utf16.Decode(units)), `encoding="UTF-16"`) {
		t.Fatal("XML declaration does not match bytes")
	}
	failLegacy := func([]byte) ([]byte, error) { t.Fatal("UTF-16 must not use OEM decoding"); return nil, nil }
	for _, variant := range [][]byte{raw, raw[2:]} {
		decoded, err := schedulerText(variant, failLegacy)
		if err != nil {
			t.Fatal(err)
		}
		var task struct{ Command string }
		if err = xml.Unmarshal(decoded, &task); err != nil {
			t.Fatal(err)
		}
		if task.Command != `C:\用户\测试 & 🐕\molr.exe` {
			t.Fatal(task.Command)
		}
	}
	// Big-endian XML returned by a tool must also decode independently of OEM.
	be := append([]byte{0xfe, 0xff}, raw[2:]...)
	for i := 2; i < len(be); i += 2 {
		be[i], be[i+1] = be[i+1], be[i]
	}
	if decoded, err := schedulerText(be, failLegacy); err != nil || !bytes.Equal(decoded, []byte(original)) {
		t.Fatal(string(decoded), err)
	}
}
func TestWindowsSchedulerTextEncodingSelection(t *testing.T) {
	calls := 0
	legacy := func(raw []byte) ([]byte, error) { calls++; return append([]byte("legacy:"), raw...), nil }
	raw := []byte{0xb4, 0xed, 0xce, 0xf3}
	if out, err := schedulerText(raw, legacy); err != nil || !bytes.Equal(out, append([]byte("legacy:"), raw...)) || calls != 1 {
		t.Fatal(out, err, calls)
	}
	unicode := []byte("错误：中文路径")
	if out, err := schedulerText(append([]byte{0xef, 0xbb, 0xbf}, unicode...), legacy); err != nil || !bytes.Equal(out, unicode) || calls != 1 {
		t.Fatal(string(out), err, calls)
	}
	utf8XML := []byte(`<?xml version="1.0" encoding="UTF-8"?><Task>中文</Task>`)
	if out, err := schedulerText(utf8XML, legacy); err != nil || !bytes.Equal(out, utf8XML) || calls != 1 {
		t.Fatal(string(out), err, calls)
	}
	for _, bad := range [][]byte{{0xff, 0xfe, 0x3c}, {0xff, 0xfe, 0x00, 0xd8}, {0xff, 0xfe, 0x00, 0xdc}} {
		if _, err := schedulerText(bad, legacy); err == nil {
			t.Fatal("malformed UTF-16 accepted")
		}
	}
	// Query text may already be narrowed by schtasks even with UTF-16 declared.
	query := []byte(`<?xml version="1.0" encoding="UTF-16"?><Task/>`)
	out, err := schedulerText(query, func(raw []byte) ([]byte, error) { return raw, nil })
	if err != nil || bytes.Contains(out, []byte("UTF-16")) {
		t.Fatal(string(out), err)
	}
}
func TestWindowsInitialLoginAfterInterruptedInstall(t *testing.T) {
	for _, tc := range []struct {
		name, action, receipt, bootstrap string
		want                             bool
	}{
		{"new", "install", "", "Owner: owner\n", true},
		{"registration failed after bootstrap", "install", "", "Already initialized; account and setup state preserved.\n", true},
		{"reinstall retained data", "install", `{"installed":false}`, "Already initialized; account and setup state preserved.\n", false},
		{"upgrade", "upgrade", `{"installed":true}`, "Already initialized; account and setup state preserved.\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsInitialLogin(tc.action, []byte(tc.receipt), []byte(tc.bootstrap)); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
