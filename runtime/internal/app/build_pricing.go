package app

import (
	"context"
	"io"
	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/pricing"
	"os"
	"path/filepath"
)

func (b *Bundle) buildPricing(ctx context.Context, cfg config.Config) error {
	var err error
	var exchangeRules io.Reader
	if cfg.Pricing.ExchangeRatesFile != "" {
		file, openErr := os.Open(cfg.Pricing.ExchangeRatesFile)
		if openErr != nil {
			return core.Wrap(core.InvalidArgument, "cannot open exchange rates", openErr)
		}
		defer file.Close()
		exchangeRules = file
	}
	b.Exchange, err = pricing.NewExchange(exchangeRules)
	if err != nil {
		return err
	}
	b.Pricing, err = pricing.New(nil)
	if err != nil {
		return err
	}
	b.priceUpdater, err = pricing.NewUpdater(ctx, b.Pricing, pricing.UpdateOptions{File: cfg.Pricing.RulesFile, URL: cfg.Pricing.RulesURL, CacheDir: filepath.Join(cfg.DataDir, "pricing-cache"), Client: platform.ConfiguredHTTPClient(cfg.Pricing.RulesURL, adapters{networkConfig: cfg.Network}.privateAddresses())})
	if err != nil {
		return err
	}
	return nil
}
