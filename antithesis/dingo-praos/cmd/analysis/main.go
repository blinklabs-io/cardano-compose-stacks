// Copyright 2026 Blink Labs Software
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Command analysis reads Cardano node log files from a shared volume and
// reports Antithesis property-based assertions (safety and liveness) on a
// configurable ticker.  When built without the "antithesis" build tag the
// assertions are written to stderr as structured JSON logs so that the binary
// remains useful outside the Antithesis platform.
//
// The one-shot subcommand "eventually" reads the logs once and reports the
// post-fault convergence assertion instead of starting the ticker.
//
// Configuration is read entirely from environment variables; see
// internal/analysis/config.go for the full list.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/blinklabs-io/cardano-compose-stacks/antithesis/dingo-praos/internal/analysis"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := analysis.LoadConfig()
	if err != nil {
		logger.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		os.Exit(dispatch(os.Args[1:], cfg, logger))
	}

	logger.Info(
		"analysis binary starting",
		"log_dir", cfg.LogDir,
		"initial_wait_s", int(cfg.InitialWait.Seconds()),
		"check_interval_s", int(cfg.CheckInterval.Seconds()),
		"max_fork_depth", cfg.MaxForkDepth,
		"pools", cfg.Pools,
		"min_blocks_sample", cfg.MinBlocksSample,
	)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	analyzer := analysis.NewAnalyzer(cfg, logger)

	if runErr := analyzer.Run(ctx); runErr != nil &&
		!errors.Is(runErr, context.Canceled) {
		logger.Error("analyzer exited with error", "err", runErr)
		os.Exit(1)
	}

	logger.Info("analysis stopped")
}

// dispatch runs the subcommand named by args[0]. The subcommands take no
// arguments, so any extra argument is rejected rather than ignored.
func dispatch(args []string, cfg *analysis.Config, logger *slog.Logger) int {
	if len(args) != 1 {
		logger.Error("unexpected arguments", "args", args[1:])
		return 2
	}
	return runSubcommand(args[0], cfg, logger)
}

// runSubcommand runs a one-shot command and returns the process exit code.
// The commands read the same ANALYSIS_* environment as the daemon, so one
// started with `docker exec` in the daemon's container sees the same log
// directory.
func runSubcommand(name string, cfg *analysis.Config, logger *slog.Logger) int {
	switch name {
	case "eventually":
		// The verdict is the emitted assertion; the exit code only reports
		// that the command ran.
		res := analysis.NewAnalyzer(cfg, logger).CheckEventually()
		logger.Info(
			"eventually check complete",
			"log_dir", cfg.LogDir,
			"comparable", res.Comparable,
			"converged", res.Converged,
		)
		return 0
	default:
		logger.Error("unknown subcommand", "name", name)
		return 2
	}
}
