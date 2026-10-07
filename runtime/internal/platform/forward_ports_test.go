package platform

import "testing"

func TestForwardPortsAreBoundedUniqueTCPPorts(t *testing.T) {
	for _, ports := range [][]int{nil, {80}, {80, 443, 8080, 65535}} {
		if err := ValidateForwardPorts(ports); err != nil {
			t.Fatal(ports, err)
		}
	}
	for _, ports := range [][]int{{0}, {-1}, {65536}, {80, 80}, {1, 2, 3, 4, 5}} {
		if ValidateForwardPorts(ports) == nil {
			t.Fatal("invalid forwards accepted", ports)
		}
	}
}
