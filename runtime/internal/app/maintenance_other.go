//go:build !linux && !darwin

package app

import "molr.ai/runtime/internal/maintenance"

func newMaintenance(config string, external bool) maintenance.Service {
	return maintenance.Unavailable{ExternalBrowser: external}
}
