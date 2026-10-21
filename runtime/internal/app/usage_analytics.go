package app

import (
	"context"
	"math"
	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/api"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/pricing"
	"molr.ai/runtime/internal/quota"
	"sort"
	"strings"
	"time"
)

type analyticsTotals struct {
	ReferenceCalls                                               int64
	InputTokens, OutputTokens, CachedTokens, TotalTokens         int64
	ModelCalls, MissingTokenCalls, UnknownCostCalls, ActiveCalls int64
	Spent                                                        []core.Money
}
type analyticsRow struct {
	ID                     core.ID
	Name, Secondary, State string
	analyticsTotals
}
type analyticsDay struct {
	Date string
	analyticsTotals
}
type analyticsOption struct {
	ID                     core.ID
	Name, Secondary, State string
}
type analyticsOptions struct{ Assistants, Models, Users, Groups []analyticsOption }
type analyticsView struct {
	From, Until, Timezone     string
	AsOf                      time.Time
	Currency                  string
	Rates                     pricing.ExchangeSnapshot
	Summary                   analyticsTotals
	ActiveDays, ActiveUsers   int
	Daily                     []analyticsDay
	Rows                      []analyticsRow
	TotalRows, Page, PageSize int
	Options                   analyticsOptions
}

func emptyAnalytics() analyticsTotals { return analyticsTotals{Spent: []core.Money{}} }
func (a *analyticsTotals) add(b analyticsTotals) error {
	for _, pair := range []struct {
		to    *int64
		value int64
	}{{&a.ReferenceCalls, b.ReferenceCalls}, {&a.InputTokens, b.InputTokens}, {&a.OutputTokens, b.OutputTokens}, {&a.CachedTokens, b.CachedTokens}, {&a.TotalTokens, b.TotalTokens}, {&a.ModelCalls, b.ModelCalls}, {&a.MissingTokenCalls, b.MissingTokenCalls}, {&a.UnknownCostCalls, b.UnknownCostCalls}, {&a.ActiveCalls, b.ActiveCalls}} {
		if pair.value < 0 || *pair.to > math.MaxInt64-pair.value {
			return core.NewError(core.Internal, "usage overflow")
		}
		*pair.to += pair.value
	}
	for _, amount := range b.Spent {
		if err := amount.Validate(); err != nil {
			return err
		}
		found := false
		for i := range a.Spent {
			if a.Spent[i].Currency == amount.Currency {
				if a.Spent[i].Micros > math.MaxInt64-amount.Micros {
					return core.NewError(core.Internal, "usage overflow")
				}
				a.Spent[i].Micros += amount.Micros
				found = true
				break
			}
		}
		if !found {
			a.Spent = append(a.Spent, amount)
		}
	}
	sort.Slice(a.Spent, func(i, j int) bool { return a.Spent[i].Currency < a.Spent[j].Currency })
	return nil
}
func usageAnalytics(ctx context.Context, store persistence.Store, identity *auth.Manager, p core.Principal, q api.UsageQuery, admin bool, currency string, rates pricing.ExchangeSnapshot) (analyticsView, error) {
	out := analyticsView{AsOf: time.Now().UTC(), Currency: currency, Rates: rates, Summary: emptyAnalytics(), Daily: []analyticsDay{}, Rows: []analyticsRow{}, Page: q.Page, PageSize: 20, Options: analyticsOptions{Assistants: []analyticsOption{}, Models: []analyticsOption{}, Users: []analyticsOption{}, Groups: []analyticsOption{}}}
	if admin {
		if err := identity.RequireAdministrator(ctx, p); err != nil {
			return out, err
		}
	} else {
		if err := p.RequireOwner(); err != nil {
			return out, err
		}
		if q.UserID != "" || q.GroupID != "" || q.Search != "" {
			return out, core.Invalid("scope", "member filters require administrator")
		}
	}
	if q.Timezone == "" {
		q.Timezone = "UTC"
	}
	loc, err := time.LoadLocation(q.Timezone)
	if err != nil {
		return out, core.Invalid("timezone", "invalid timezone")
	}
	from, err := time.ParseInLocation("2006-01-02", q.From, loc)
	if err != nil {
		return out, core.Invalid("from", "calendar date required")
	}
	until, err := time.ParseInLocation("2006-01-02", q.Until, loc)
	if err != nil || !until.After(from) || until.After(from.AddDate(1, 0, 0)) {
		return out, core.Invalid("until", "range must be at most one year")
	}
	if q.Day != "" {
		day, e := time.ParseInLocation("2006-01-02", q.Day, loc)
		if e != nil || day.Before(from) || !day.Before(until) {
			return out, core.Invalid("day", "day outside range")
		}
	}
	if q.GroupBy == "" {
		if admin {
			q.GroupBy = "user"
		} else {
			q.GroupBy = "assistant"
		}
	}
	if q.GroupBy != "model" && !(admin && (q.GroupBy == "user" || q.GroupBy == "group")) && !(!admin && q.GroupBy == "assistant") {
		return out, core.Invalid("group_by", "unsupported grouping")
	}
	if admin && q.AssistantID != "" {
		return out, core.Invalid("assistant_id", "private assistant filter unavailable")
	}
	if q.Sort == "" {
		q.Sort = "tokens"
	}
	if q.Sort != "tokens" && q.Sort != "spend" && q.Sort != "calls" {
		return out, core.Invalid("sort", "unsupported sort")
	}
	if out.Page < 1 {
		out.Page = 1
	}
	out.From, out.Until, out.Timezone = q.From, q.Until, q.Timezone
	dayIndex := map[string]int{}
	for d := from; d.Before(until); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		dayIndex[key] = len(out.Daily)
		out.Daily = append(out.Daily, analyticsDay{key, emptyAnalytics()})
	}
	err = store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		users := []auth.UserInfo{}
		if admin {
			page := core.PageRequest{Limit: 200}
			for {
				v, e := identity.Users(ctx, p, page)
				if e != nil {
					return e
				}
				users = append(users, v.Items...)
				if v.NextCursor == "" {
					break
				}
				page.Cursor = v.NextCursor
			}
			groups, e := identity.Groups(ctx, p)
			if e != nil {
				return e
			}
			for _, g := range groups {
				out.Options.Groups = append(out.Options.Groups, analyticsOption{ID: g.ID, Name: g.Name})
			}
		} else {
			u, e := identity.CurrentUser(ctx, p)
			if e != nil {
				return e
			}
			users = append(users, u)
		}
		groups := map[core.ID]string{}
		for _, g := range out.Options.Groups {
			groups[g.ID] = g.Name
		}
		assistants := map[core.ID]analyticsOption{}
		if !admin {
			items, e := persistence.Select[analyticsOption](ctx, tx.Store(), "business.assistants", persistence.Query{Fields: []string{"ID", "Name", "State"}, Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID)}})
			if e != nil {
				return e
			}
			for _, a := range items {
				assistants[a.ID] = a
			}
		}
		models := map[core.ID]analyticsOption{}
		rows := map[core.ID]*analyticsRow{}
		activeUsers := map[core.ID]bool{}
		seenAssistants := map[core.ID]bool{}
		for _, u := range users {
			name := u.DisplayName
			if name == "" {
				name = u.Username
			}
			if admin {
				out.Options.Users = append(out.Options.Users, analyticsOption{ID: u.ID, Name: name, Secondary: u.Username})
			}
			if (q.UserID != "" && q.UserID != u.ID) || (q.GroupID != "" && q.GroupID != u.GroupID) || !strings.Contains(strings.ToLower(name+" "+u.Username), strings.ToLower(strings.TrimSpace(q.Search))) {
				continue
			}
			records, e := persistence.Select[quota.ModelRecord](ctx, tx.Store(), "security.reservations", persistence.Query{Unordered: true, Fields: []string{"OwnerID", "AssistantID", "ModelID", "Reservation", "ReceiptID", "CreatedAt"}, Where: []persistence.Filter{persistence.Eq("OwnerID", u.ID), {Field: "CreatedAt", Op: ">=", Value: from.Add(-time.Millisecond).UTC().Format(time.RFC3339Nano)}, {Field: "CreatedAt", Op: "<", Value: until.Add(time.Millisecond).UTC().Format(time.RFC3339Nano)}}})
			if e != nil {
				return e
			}
			// The reservation is the unique accounting unit. Reused model receipts do
			// not add calls; late evidence remains attributed to the original start.
			for _, r := range records {
				if !quota.InPeriod(r.CreatedAt, from, until) || r.Reservation.State == "released" {
					continue
				}
				var receipt model.Receipt
				if r.ReceiptID == "" {
					index, lookupErr := persistence.Load[struct{ ID core.ID }](ctx, tx.Store(), "agent.model_receipt_index", persistence.Key(u.ID, r.Reservation.OperationID))
					if lookupErr != nil && !persistence.Missing(lookupErr) {
						return lookupErr
					}
					r.ReceiptID = index.ID
				}
				if r.ReceiptID != "" {
					receipt, e = persistence.Load[model.Receipt](ctx, tx.Store(), "agent.model_receipts", persistence.Key(u.ID, r.ReceiptID))
					if e != nil && !persistence.Missing(e) {
						return e
					}
				}
				label := receipt.ModelName
				if label == "" {
					label = string(r.ModelID)
				}
				models[r.ModelID] = analyticsOption{ID: r.ModelID, Name: label}
				if !admin {
					seenAssistants[r.AssistantID] = true
				}
				if (q.ModelID != "" && q.ModelID != r.ModelID) || (q.AssistantID != "" && q.AssistantID != r.AssistantID) {
					continue
				}
				value := emptyAnalytics()
				value.ModelCalls = 1
				if receipt.PricingInfo != nil && receipt.PricingInfo.Basis == "subscription-reference" {
					value.ReferenceCalls = 1
				}
				if receipt.ID != "" && receipt.OutcomeKnown {
					if receipt.Usage.InputTokens < 0 || receipt.Usage.OutputTokens < 0 || receipt.Usage.CachedTokens < 0 || receipt.Usage.CachedTokens > receipt.Usage.InputTokens || receipt.Usage.InputTokens > math.MaxInt64-receipt.Usage.OutputTokens {
						return core.NewError(core.Internal, "invalid recorded usage")
					}
					value.InputTokens = receipt.Usage.InputTokens
					value.OutputTokens = receipt.Usage.OutputTokens
					value.CachedTokens = receipt.Usage.CachedTokens
					value.TotalTokens = value.InputTokens + value.OutputTokens
				} else if r.Reservation.State != "reserved" {
					value.MissingTokenCalls = 1
				}
				if r.Reservation.Settled != nil {
					value.Spent = append(value.Spent, *r.Reservation.Settled)
				} else if r.Reservation.State == "reserved" {
					value.ActiveCalls = 1
				} else {
					value.UnknownCostCalls = 1
				}
				if e = out.Summary.add(value); e != nil {
					return e
				}
				day := r.CreatedAt.In(loc).Format("2006-01-02")
				if e = out.Daily[dayIndex[day]].add(value); e != nil {
					return e
				}
				activeUsers[u.ID] = true
				if q.Day != "" && q.Day != day {
					continue
				}
				id, rowName, secondary, state := r.ModelID, label, "", ""
				switch q.GroupBy {
				case "assistant":
					id = r.AssistantID
					a, ok := assistants[id]
					if ok {
						rowName, state = a.Name, a.State
					} else {
						rowName, state = string(id), "deleted"
					}
				case "user":
					id, rowName, secondary = u.ID, name, u.Username
				case "group":
					id, rowName = u.GroupID, groups[u.GroupID]
				}
				if rows[id] == nil {
					rows[id] = &analyticsRow{ID: id, Name: rowName, Secondary: secondary, State: state, analyticsTotals: emptyAnalytics()}
				}
				if e = rows[id].add(value); e != nil {
					return e
				}
			}
		}
		for id := range seenAssistants {
			a, ok := assistants[id]
			if !ok {
				a = analyticsOption{ID: id, Name: string(id), State: "deleted"}
			}
			out.Options.Assistants = append(out.Options.Assistants, a)
		}
		for _, v := range models {
			out.Options.Models = append(out.Options.Models, v)
		}
		for _, v := range rows {
			out.Rows = append(out.Rows, *v)
		}
		out.ActiveUsers = len(activeUsers)
		return nil
	})
	if err != nil {
		return out, err
	}
	for _, d := range out.Daily {
		if d.ModelCalls > 0 {
			out.ActiveDays++
		}
	}
	for _, items := range [][]analyticsOption{out.Options.Assistants, out.Options.Models, out.Options.Users, out.Options.Groups} {
		sort.Slice(items, func(i, j int) bool {
			if items[i].Name == items[j].Name {
				return items[i].ID < items[j].ID
			}
			return items[i].Name < items[j].Name
		})
	}
	scores := map[core.ID]int64{}
	for _, r := range out.Rows {
		score := r.TotalTokens
		if q.Sort == "calls" {
			score = r.ModelCalls
		}
		if q.Sort == "spend" {
			score = 0
			for _, amount := range r.Spent {
				v, e := rates.Convert(amount, currency, true)
				if e != nil {
					return out, e
				}
				if score > math.MaxInt64-v.Micros {
					return out, core.NewError(core.Internal, "usage overflow")
				}
				score += v.Micros
			}
		}
		scores[r.ID] = score
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if scores[a.ID] == scores[b.ID] {
			return a.ID < b.ID
		}
		return scores[a.ID] > scores[b.ID]
	})
	out.TotalRows = len(out.Rows)
	start := min((out.Page-1)*out.PageSize, len(out.Rows))
	out.Rows = out.Rows[start:min(start+out.PageSize, len(out.Rows))]
	return out, nil
}
