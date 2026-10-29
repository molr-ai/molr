// Package accountview defines account administration and usage read models.
package accountview

import (
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/contextstore"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/pricing"
	"molr.ai/runtime/internal/quota"
	"time"
)

type AnalyticsTotals struct {
	ReferenceCalls                                               int64
	InputTokens, OutputTokens, CachedTokens, TotalTokens         int64
	ModelCalls, MissingTokenCalls, UnknownCostCalls, ActiveCalls int64
	Spent                                                        []core.Money
}

type AnalyticsRow struct {
	ID                     core.ID
	Name, Secondary, State string
	AnalyticsTotals
}

type AnalyticsDay struct {
	Date string
	AnalyticsTotals
}

type AnalyticsOption struct {
	ID                     core.ID
	Name, Secondary, State string
}

type AnalyticsOptions struct{ Assistants, Models, Users, Groups []AnalyticsOption }

type AnalyticsView struct {
	From, Until, Timezone     string
	AsOf                      time.Time
	Currency                  string
	Rates                     pricing.ExchangeSnapshot
	Summary                   AnalyticsTotals
	ActiveDays, ActiveUsers   int
	Daily                     []AnalyticsDay
	Rows                      []AnalyticsRow
	TotalRows, Page, PageSize int
	Options                   AnalyticsOptions
}

type UsageSummary struct {
	UserID                                                  core.ID
	Username, DisplayName                                   string
	GroupID                                                 core.ID
	ModelCalls, ToolCalls, ConnectionCalls, AttachmentBytes int64
	UserMessages                                            int64
	Active, Unknown                                         int
	Spent, Reserved                                         []core.Money
}

type AdminUsage struct {
	Users          []UsageSummary
	ContextStorage contextstore.Usage
}

type AccountQuotas struct {
	Group          auth.UserGroup
	Summary        UsageSummary
	ContextStorage contextstore.Usage
	Resources      []ResourceUsage
	AsOf           time.Time
}

type ResourceUsage struct {
	Name string
	Kind string
	ID   core.ID
	quota.Totals
	PeriodStart time.Time
	PeriodEnd   *time.Time
}

type CatalogItem struct {
	GPUAccessMode           string               `json:",omitempty"`
	GPUs                    []platform.GPUDevice `json:",omitempty"`
	FullAccessAvailable     bool
	Kind                    string
	ID                      core.ID
	Name                    string
	Limits                  *platform.Limits `json:",omitempty"`
	WorkspaceQuotaSupported bool
}

// UsageQuery selects calendar dates in Timezone; Until is exclusive.
type UsageQuery struct {
	From, Until, Timezone, GroupBy, Sort, Day, Search string
	AssistantID, ModelID, UserID, GroupID             core.ID
	Page                                              int
}
