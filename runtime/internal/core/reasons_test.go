package core

import (
	"fmt"
	"testing"
)

func TestReasonsPreserveCodesAndPrivateCauses(t *testing.T) {
	cause := NewError(FailedPrecondition, "PRIVATE diagnostic")
	err := WithReason(cause, ModelPricingMissing)
	err = fmt.Errorf("wrapper: %w", err)
	if CodeOf(err) != FailedPrecondition || ReasonOf(err) != ModelPricingMissing {
		t.Fatal("lost structured failure")
	}
	if ReasonOf(WithReason(err, ModelUnavailable)) != ModelPricingMissing {
		t.Fatal("overwrote specific reason")
	}
	if WithReason(nil, ModelUnavailable) != nil || ReasonOf(cause) != "" {
		t.Fatal("invented reason")
	}
}
