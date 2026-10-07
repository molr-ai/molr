// Package pricing loads versioned, immutable provider/model tariff snapshots.
package pricing

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
	_ "time/tzdata"

	"molr.ai/runtime/internal/core"
)

//go:embed deepseek.json
var builtin []byte

type Rates struct {
	Input  int64 `json:"input_micros_per_million"`
	Output int64 `json:"output_micros_per_million"`
	Cached int64 `json:"cached_input_micros_per_million"`
}

func (r *Rates) UnmarshalJSON(data []byte) error {
	var fields struct {
		Input  *int64 `json:"input_micros_per_million"`
		Output *int64 `json:"output_micros_per_million"`
		Cached *int64 `json:"cached_input_micros_per_million"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&fields) != nil || fields.Input == nil || fields.Output == nil || fields.Cached == nil {
		return core.Invalid("pricing", "all three integer rates are required")
	}
	*r = Rates{*fields.Input, *fields.Output, *fields.Cached}
	return nil
}

type Quote struct {
	Current, Reserve                                              Rates
	Currency, Provider, Model, RuleID, Version, Period, SourceURL string
	CalendarKnown                                                 bool
}
type window struct{ Start, End string }
type schedule struct {
	Weekdays         []int    `json:"peak_weekdays_utc"`
	Windows          []window `json:"peak_windows_utc"`
	HolidayTimezone  string   `json:"holiday_timezone"`
	HolidaySourceURL string   `json:"holiday_source_url"`
	HolidayYears     []int    `json:"holiday_years"`
	Holidays         []string `json:"holidays"`
}
type modelRule struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases"`
	OffPeak *Rates   `json:"off_peak"`
	Peak    *Rates   `json:"peak"`
}
type providerRule struct {
	ID        string      `json:"id"`
	Origins   []string    `json:"origins"`
	SourceURL string      `json:"source_url"`
	Schedule  schedule    `json:"schedule"`
	Models    []modelRule `json:"models"`
}
type document struct {
	SchemaVersion int            `json:"schema_version"`
	Version       string         `json:"version"`
	Currency      string         `json:"currency"`
	Providers     []providerRule `json:"providers"`
}
type provider struct {
	rule         providerRule
	location     *time.Location
	models       map[string]modelRule
	holidayYears map[int]bool
	holidays     map[string]bool
	weekdays     map[time.Weekday]bool
	windows      [][2]int
}
type snapshot struct {
	version, currency string
	providers         []provider
}
type Catalog struct{ current atomic.Pointer[snapshot] }

// New loads the embedded rules when r is nil; an external file replaces them.
func New(r io.Reader) (*Catalog, error) {
	c := &Catalog{}
	if r == nil {
		r = bytes.NewReader(builtin)
	}
	if err := c.Reload(r); err != nil {
		return nil, err
	}
	return c, nil
}

// Reload validates the complete replacement before atomically publishing it.
// Errors leave the last snapshot intact. Existing Quotes never change.
// A future server-side updater can call this without restarting agent workers.
func (c *Catalog) Reload(r io.Reader) error {
	if r == nil {
		return core.Invalid("pricing", "rules reader required")
	}
	data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return core.Invalid("pricing", "rules too large")
	}
	var doc document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return core.Invalid("pricing", "invalid rules JSON")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return core.Invalid("pricing", "trailing rules data")
	}
	if doc.SchemaVersion != 1 || strings.TrimSpace(doc.Version) == "" || len(doc.Version) > 128 || doc.Currency != "USD" || len(doc.Providers) == 0 || len(doc.Providers) > 100 {
		return core.Invalid("pricing", "invalid rules metadata")
	}
	next := &snapshot{version: doc.Version, currency: doc.Currency}
	seen := map[string]bool{}
	for _, rule := range doc.Providers {
		if rule.ID == "" || seen[rule.ID] || len(rule.Models) == 0 || len(rule.Origins) == 0 {
			return core.Invalid("pricing", "invalid provider")
		}
		seen[rule.ID] = true
		for _, origin := range rule.Origins {
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				return core.Invalid("pricing", "exact HTTPS origin required")
			}
		}
		p := provider{rule: rule, models: map[string]modelRule{}, holidays: map[string]bool{}, holidayYears: map[int]bool{}, weekdays: map[time.Weekday]bool{}}
		p.location, err = time.LoadLocation(rule.Schedule.HolidayTimezone)
		if err != nil || rule.Schedule.HolidayTimezone == "" {
			return core.Invalid("pricing", "holiday timezone required")
		}
		for _, year := range rule.Schedule.HolidayYears {
			if year < 2000 || year > 2200 {
				return core.Invalid("pricing", "invalid holiday year")
			}
			p.holidayYears[year] = true
		}
		for _, day := range rule.Schedule.Holidays {
			date, err := time.Parse("2006-01-02", day)
			if err != nil || !p.holidayYears[date.Year()] {
				return core.Invalid("pricing", "invalid holiday date")
			}
			p.holidays[day] = true
		}
		for _, day := range rule.Schedule.Weekdays {
			if day < 0 || day > 6 {
				return core.Invalid("pricing", "invalid weekday")
			}
			p.weekdays[time.Weekday(day)] = true
		}
		for _, w := range rule.Schedule.Windows {
			start, e1 := time.Parse("15:04", w.Start)
			end, e2 := time.Parse("15:04", w.End)
			a, b := start.Hour()*60+start.Minute(), end.Hour()*60+end.Minute()
			if e1 != nil || e2 != nil || a >= b {
				return core.Invalid("pricing", "invalid peak window")
			}
			for _, old := range p.windows {
				if a < old[1] && b > old[0] {
					return core.Invalid("pricing", "overlapping peak windows")
				}
			}
			p.windows = append(p.windows, [2]int{a, b})
		}
		for _, m := range rule.Models {
			if m.OffPeak == nil || m.Peak == nil {
				return core.Invalid("pricing", "peak and off-peak rates required")
			}
			for _, rates := range []*Rates{m.OffPeak, m.Peak} {
				if rates.Input < 0 || rates.Output < 0 || rates.Cached < 0 || rates.Cached > rates.Input {
					return core.Invalid("pricing", "invalid rates")
				}
			}
			if m.Peak.Input < m.OffPeak.Input || m.Peak.Output < m.OffPeak.Output || m.Peak.Cached < m.OffPeak.Cached {
				return core.Invalid("pricing", "peak rates must bound estimates")
			}
			for _, name := range append([]string{m.ID}, m.Aliases...) {
				if name == "" || len(name) > 256 || strings.TrimSpace(name) != name {
					return core.Invalid("pricing", "invalid model name")
				}
				if _, exists := p.models[name]; exists {
					return core.Invalid("pricing", "duplicate model or alias")
				}
				p.models[name] = m
			}
		}
		next.providers = append(next.providers, p)
	}
	c.current.Store(next)
	return nil
}

// Lookup requires an official configured origin as well as a known model name.
// Empty hint auto-detects an official provider; "custom" disables built-in rules.
func (c *Catalog) Lookup(hint, endpoint, model string, at time.Time) (Quote, bool) {
	if c == nil || hint == "custom" {
		return Quote{}, false
	}
	s := c.current.Load()
	if s == nil {
		return Quote{}, false
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return Quote{}, false
	}
	origin := u.Scheme + "://" + strings.ToLower(u.Host)
	for _, p := range s.providers {
		if hint != "" && hint != p.rule.ID {
			continue
		}
		matched := false
		for _, allowed := range p.rule.Origins {
			if origin == allowed {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		m, ok := p.models[model]
		if !ok {
			return Quote{}, false
		}
		utc, local := at.UTC(), at.In(p.location)
		peak := false
		if p.weekdays[utc.Weekday()] && !p.holidays[local.Format("2006-01-02")] {
			minute := utc.Hour()*60 + utc.Minute()
			for _, w := range p.windows {
				if minute >= w[0] && minute < w[1] {
					peak = true
				}
			}
		}
		rates, period := m.OffPeak, "off_peak"
		if peak {
			rates, period = m.Peak, "peak"
		}
		return Quote{Current: *rates, Reserve: *m.Peak, Currency: s.currency, Provider: p.rule.ID, Model: m.ID, RuleID: fmt.Sprintf("%s_%s", p.rule.ID, m.ID), Version: s.version, Period: period, SourceURL: p.rule.SourceURL, CalendarKnown: p.holidayYears[local.Year()]}, true
	}
	return Quote{}, false
}
