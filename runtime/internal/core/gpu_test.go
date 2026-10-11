package core

import "testing"

func TestGPUDevicesAreExplicitAndBounded(t *testing.T) {
	a := "GPU-00000000-0000-0000-0000-000000000001"
	b := "GPU-00000000-0000-0000-0000-000000000002"
	if err := CheckGPUDevices([]string{a}, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{a, a}, {"all"}, {"/dev/nvidia0"}, {b}} {
		if CheckGPUDevices(ids, []string{a}) == nil {
			t.Fatalf("accepted %v", ids)
		}
	}
	if CheckGPUDevices(nil, nil) != nil {
		t.Fatal("default CPU access blocked")
	}
	var c *ComputeConstraints
	if len(c.GPUDevices()) != 0 {
		t.Fatal("nil compute includes devices")
	}
}
