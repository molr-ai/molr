package maintenance

import (
	"errors"
	"testing"
)

func TestUnavailableMaintenanceDoesNotOfferOrStartHostOperations(t *testing.T) {
	for _, external := range []bool{false, true} {
		var service Service = Unavailable{ExternalBrowser: external}
		if service.Enabled() || service.CanUpdate(t.Context()) || service.Latest(t.Context()) != nil || len(service.ProtectedRoots()) != 0 {
			t.Fatal("unsupported maintenance advertised host integration")
		}
		status := service.Snapshot(t.Context())
		want := "unsupported"
		if external {
			want = "external"
		}
		if len(status) != 1 || status[0].State != want || status[0].CanInstall || status[0].AutoInstall || status[0].Task != nil {
			t.Fatal(status)
		}
		_, _, programErr := service.Programs()
		_, taskErr := service.Task(t.Context(), "task")
		_, startErr := service.Start(t.Context(), "install", "browser", "key", "")
		_, cancelErr := service.Cancel(t.Context(), "task")
		for _, err := range []error{programErr, taskErr, startErr, cancelErr} {
			var unavailable *ComponentError
			if !errors.As(err, &unavailable) || unavailable.Code != "maintenance_platform_unsupported" || err.Error() == "" {
				t.Fatal(err)
			}
		}
	}
}
