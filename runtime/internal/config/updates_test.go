package config

import "testing"

func TestUpdateSources(t *testing.T) {
	for _, source := range []string{"https://example.com/releases", "http://127.0.0.1:9999/releases", "http://192.168.1.20/releases", ""} {
		c := Defaults(t.TempDir())
		c.HTTP.TLSMode = LocalHTTP
		c.HTTP.Listen = "127.0.0.1:8787"
		c.HTTP.PublicURL = "http://127.0.0.1:8787"
		c.Updates.Source = source
		if err := c.Validate(); err != nil {
			t.Fatal(source, err)
		}
	}
	for _, source := range []string{"http://example.com/releases", "https://name:secret@example.com", "https://example.com/?token=x", "file:///tmp/releases", "http://169.254.169.254/releases"} {
		c := Defaults(t.TempDir())
		c.Updates.Source = source
		if err := c.Validate(); err == nil {
			t.Fatal(source)
		}
	}
}
