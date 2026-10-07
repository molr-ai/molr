package pricing

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"math/big"
	"net/url"
	"sync/atomic"
	"time"

	"molr.ai/runtime/internal/core"
)

//go:embed exchange.json
var builtinExchange []byte

// ExchangeSnapshot stores proportional integer currency units from one source.
// Only the ratio matters: USD=11367, CNY=76302 represents the ECB EUR crosses.
type ExchangeSnapshot struct {
	Version   string           `json:"version"`
	AsOf      string           `json:"as_of"`
	SourceURL string           `json:"source_url"`
	Units     map[string]int64 `json:"units"`
}
type ExchangeReader interface{ Snapshot() ExchangeSnapshot }

type Exchange struct {
	current atomic.Pointer[ExchangeSnapshot]
}

func NewExchange(r io.Reader) (*Exchange, error) {
	e := &Exchange{}
	if r == nil {
		r = bytes.NewReader(builtinExchange)
	}
	if err := e.Reload(r); err != nil {
		return nil, err
	}
	return e, nil
}
func (e *Exchange) Reload(r io.Reader) error {
	if r == nil {
		return core.Invalid("exchange", "reader required")
	}
	data, err := io.ReadAll(io.LimitReader(r, 16385))
	if err != nil {
		return err
	}
	if len(data) > 16384 {
		return core.Invalid("exchange", "rates too large")
	}
	var next ExchangeSnapshot
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&next) != nil || d.Decode(new(any)) != io.EOF {
		return core.Invalid("exchange", "invalid JSON")
	}
	u, err := url.Parse(next.SourceURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || next.Version == "" || len(next.Version) > 128 {
		return core.Invalid("exchange", "source and version required")
	}
	if _, err := time.Parse("2006-01-02", next.AsOf); err != nil {
		return core.Invalid("exchange", "invalid date")
	}
	if len(next.Units) != 2 || next.Units["USD"] <= 0 || next.Units["CNY"] <= 0 || next.Units["USD"] > 1e12 || next.Units["CNY"] > 1e12 {
		return core.Invalid("exchange", "positive USD and CNY units required")
	}
	e.current.Store(&next)
	return nil
}
func (e *Exchange) Snapshot() ExchangeSnapshot {
	if e == nil {
		return ExchangeSnapshot{}
	}
	s := e.current.Load()
	if s == nil {
		return ExchangeSnapshot{}
	}
	out := *s
	out.Units = map[string]int64{}
	for k, v := range s.Units {
		out.Units[k] = v
	}
	return out
}
func (s ExchangeSnapshot) Convert(amount core.Money, target string, roundUp bool) (core.Money, error) {
	if err := amount.Validate(); err != nil {
		return core.Money{}, err
	}
	from, to := s.Units[amount.Currency], s.Units[target]
	if from <= 0 || to <= 0 {
		return core.Money{}, core.Invalid("currency", "unsupported conversion")
	}
	total := new(big.Int).Mul(big.NewInt(amount.Micros), big.NewInt(to))
	if roundUp {
		total.Add(total, big.NewInt(from-1))
	}
	total.Div(total, big.NewInt(from))
	if !total.IsInt64() {
		return core.Money{}, core.Invalid("amount", "conversion overflow")
	}
	return core.Money{Currency: target, Micros: total.Int64()}, nil
}
