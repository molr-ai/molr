package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestLoggingPolicyDefaultsAndJSONOverrides(t *testing.T) {
	const base = `{"data_dir":"/var/lib/molr","http":{"public_url":"https://host.test","tls_mode":"proxy","trusted_proxies":["192.0.2.1/32"]}%s}`
	for _, tc := range []struct {
		json    string
		bytes   int64
		backups int
		invalid bool
	}{
		{"", 256 << 20, 1, false},
		{`,"logging":{"max_file_bytes":1048576,"backup_files":3}`, 1 << 20, 3, false},
		{`,"logging":{"backup_files":0}`, 256 << 20, 0, false},
		{`,"logging":{"max_file_bytes":0}`, 0, 0, true},
		{`,"logging":{"max_file_bytes":-1}`, 0, 0, true},
		{`,"logging":{"backup_files":-1}`, 0, 0, true},
		{`,"logging":{"backup_files":101}`, 0, 0, true},
		{`,"logging":{"max_file_bytes":"256M"}`, 0, 0, true},
		{`,"logging":{"unknown":true}`, 0, 0, true},
	} {
		c, err := Load(strings.NewReader(fmt.Sprintf(base, tc.json)))
		if tc.invalid {
			if err == nil {
				t.Fatalf("accepted %s", tc.json)
			}
			continue
		}
		if err != nil || c.Logging.MaxFileBytes != tc.bytes || c.Logging.BackupFiles != tc.backups {
			t.Fatalf("policy %s: %+v, %v", tc.json, c.Logging, err)
		}
	}
}
