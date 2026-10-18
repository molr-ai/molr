package installationwindows

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// schtasks loads the XML file into a Unicode string before registering it. Use
// a UTF-16 declaration with actual UTF-16LE bytes and a BOM, including non-ASCII
// paths. Internal task construction and comparison continue to use UTF-8.
func taskXMLFile(xml string) []byte {
	xml = strings.Replace(xml, `encoding="UTF-8"`, `encoding="UTF-16"`, 1)
	units := utf16.Encode([]rune(xml))
	out := make([]byte, 2+2*len(units))
	out[0], out[1] = 0xff, 0xfe
	for i, unit := range units {
		binary.LittleEndian.PutUint16(out[2+i*2:], unit)
	}
	return out
}

var utf16Declaration = regexp.MustCompile(`(?i)(<\?xml\s[^?]*\bencoding\s*=\s*["'])UTF-16(?:LE|BE)?(["'])`)
var utf8Declaration = regexp.MustCompile(`(?i)^<\?xml\s[^?]*\bencoding\s*=\s*["']UTF-8["']`)

// UTF-16 /Query XML and BOM-tagged output take priority over the console/OEM
// code page. Decode streams separately: stderr may be OEM while stdout is XML.
func schedulerText(raw []byte, legacy func([]byte) ([]byte, error)) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var order binary.ByteOrder
	switch {
	case bytes.HasPrefix(raw, []byte{0xff, 0xfe}):
		order = binary.LittleEndian
		raw = raw[2:]
	case bytes.HasPrefix(raw, []byte{0xfe, 0xff}):
		order = binary.BigEndian
		raw = raw[2:]
	case bytes.HasPrefix(raw, []byte{'<', 0, '?', 0}):
		order = binary.LittleEndian
	case bytes.HasPrefix(raw, []byte{0, '<', 0, '?'}):
		order = binary.BigEndian
	}
	var text []byte
	if order != nil {
		if len(raw)%2 != 0 {
			return nil, fmt.Errorf("truncated UTF-16 scheduler output")
		}
		units := make([]uint16, len(raw)/2)
		for i := range units {
			units[i] = order.Uint16(raw[i*2:])
		}
		for i := 0; i < len(units); i++ {
			u := units[i]
			if 0xd800 <= u && u <= 0xdbff {
				if i+1 >= len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
					return nil, fmt.Errorf("invalid UTF-16 scheduler output")
				}
				i++
			} else if 0xdc00 <= u && u <= 0xdfff {
				return nil, fmt.Errorf("invalid UTF-16 scheduler output")
			}
		}
		text = []byte(string(utf16.Decode(units)))
	} else if bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) || utf8Declaration.Match(raw) {
		text = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
		if !utf8.Valid(text) {
			return nil, fmt.Errorf("invalid UTF-8 scheduler output")
		}
	} else {
		var err error
		text, err = legacy(raw)
		if err != nil {
			return nil, err
		}
	}
	return utf16Declaration.ReplaceAll(text, []byte(`${1}UTF-8${2}`)), nil
}

// Registration can fail after bootstrap has already created the account. An
// absent receipt identifies an unfinished first install; an uninstall retains
// its receipt, so reinstalling retained data never reuses initial credentials.
func needsInitialLogin(action string, initialRecord, bootstrap []byte) bool {
	return strings.HasPrefix(string(bootstrap), "Owner: ") || action == "install" && len(initialRecord) == 0
}
