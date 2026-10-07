package config

import "testing"

func TestForwardConfigBoundaries(t *testing.T) {
	for _, host := range []string{"example.com", "127.0.0.1", "192.168.1.10", "::1"} {
		if err := (PortForward{Enabled: true, ListenHost: "0.0.0.0", PublicHost: host}).validate(); err != nil {
			t.Fatal(host, err)
		}
	}
	for _, host := range []string{"http://example.com", "example.com:8080", "0.0.0.0", "::", "x/y", "x?y", "x#y", "x@y", "a b", "224.0.0.1"} {
		if err := (PortForward{Enabled: true, ListenHost: "0.0.0.0", PublicHost: host}).validate(); err == nil {
			t.Fatal("accepted", host)
		}
	}
	if err := (PortForward{Enabled: true, ListenHost: "0.0.0.0:3000"}).validate(); err == nil {
		t.Fatal("accepted listen port")
	}
	if !Defaults("/tmp/test").PortForward.Enabled {
		t.Fatal("forward unavailable by default")
	}
}
