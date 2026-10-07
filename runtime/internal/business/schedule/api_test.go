package schedule

import (
	"testing"
	"time"
)

func TestCatchUpIsBoundedAndTimezoneIsExplicit(t *testing.T) {
	trigger := Trigger{StartAt: time.Now(), Timezone: "UTC", MissedPolicy: CatchUp, MaxCatchUp: 5}
	if err := trigger.Validate(); err != nil {
		t.Fatal(err)
	}
	trigger.MaxCatchUp = 0
	if trigger.Validate() == nil {
		t.Fatal("unbounded catch-up accepted")
	}
	trigger.MaxCatchUp = 5
	trigger.Timezone = "Not/A_Real_Zone"
	if trigger.Validate() == nil {
		t.Fatal("unknown timezone accepted")
	}
}
