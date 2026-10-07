package core

import "testing"

func TestAssistantBudgetRequiresLimitAndCalendarPeriod(t *testing.T) {
	for _, b := range []AssistantBudget{{}, {Limit: Money{Currency: "USD"}}, {Limit: Money{Currency: "EUR"}, Period: "daily"}, {Limit: Money{Currency: "USD", Micros: -1}, Period: "weekly"}, {Limit: Money{Currency: "USD"}, Period: "monthly"}} {
		if b.Validate() == nil {
			t.Fatalf("invalid budget accepted: %+v", b)
		}
	}
	if err := (AssistantBudget{Limit: Money{Currency: "CNY", Micros: 1000000}, Period: "daily"}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, period := range []string{"daily", "weekly"} {
		if err := (AssistantBudget{Limit: Money{Currency: "USD"}, Period: period}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
}
