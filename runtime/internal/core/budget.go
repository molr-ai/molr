package core

// AssistantBudget is the assistant's spending cap for a calendar period.
// Zero explicitly permits only zero-cost operations.
type AssistantBudget struct {
	Limit  Money
	Period string
}

func (b AssistantBudget) Validate() error {
	if err := b.Limit.Validate(); err != nil {
		return err
	}
	if (b.Limit.Currency != "USD" && b.Limit.Currency != "CNY") || (b.Period != "daily" && b.Period != "weekly") {
		return Invalid("budget", "USD or CNY limit and daily or weekly period required")
	}
	return nil
}
