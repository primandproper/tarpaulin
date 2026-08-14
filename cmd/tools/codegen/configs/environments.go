package main

import (
	"github.com/primandproper/tarpaulin/internal/config"

	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/logging"
	loggingcfg "github.com/primandproper/platform-go/v10/observability/logging/config"
)

// Each builder returns a fully-formed *config.Config for one environment. The
// configs are constructed here as real, typed Go objects — the compiler and the
// config's own Validate method are the guardrails — and then Render projects
// them to the JSON files under config/. Grow a builder as the application's
// Config grows (database, HTTP server, real telemetry, ...); the leftover
// observability pillars (tracing, metrics, profiling) stay at their zero values,
// which platform-go resolves to noop providers.

// buildLocalDevConfig is the config a developer runs against locally: structured
// slog logging at debug so everything is visible on the console.
//
//tarp:ignore -- a config literal in a cmd package `make test` excludes; the compiler and the Validate that Render runs are its guardrails
func buildLocalDevConfig() *config.Config {
	return &config.Config{
		Observability: observability.Config{
			Logging: loggingcfg.Config{
				Provider:    loggingcfg.ProviderSlog,
				ServiceName: config.DefaultServiceName,
				Level:       logging.DebugLevel,
			},
		},
	}
}

// buildProductionConfig is the config for a production deployment: the same
// structured slog logging, dialed back to info so logs stay signal-dense.
//
//tarp:ignore -- a config literal in a cmd package `make test` excludes; the compiler and the Validate that Render runs are its guardrails
func buildProductionConfig() *config.Config {
	return &config.Config{
		Observability: observability.Config{
			Logging: loggingcfg.Config{
				Provider:    loggingcfg.ProviderSlog,
				ServiceName: config.DefaultServiceName,
				Level:       logging.InfoLevel,
			},
		},
	}
}
