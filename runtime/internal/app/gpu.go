package app

import (
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/platform"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"slices"
)

func groupGPUInfo(info sm.Info, ref *auth.ResourceReference) sm.Info {
	if ref == nil {
		return info
	}
	allowed := ref.Quota.Compute.GPUDevices()
	ids := []string{}
	for _, id := range info.AuthorizationCeilings.GPUDeviceIDs {
		if slices.Contains(allowed, id) {
			ids = append(ids, id)
		}
	}
	info.AuthorizationCeilings.GPUDeviceIDs = ids
	devices := []platform.GPUDevice{}
	for _, d := range info.GPUs {
		if slices.Contains(ids, d.ID) {
			devices = append(devices, d)
		}
	}
	info.GPUs = devices
	return info
}
