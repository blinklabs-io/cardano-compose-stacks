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

package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/blinklabs-io/cardano-compose-stacks/antithesis/dingo-praos/internal/analysis"
)

func TestDispatchRejectsUnknownAndTrailingArguments(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &analysis.Config{LogDir: t.TempDir(), Pools: 2, ConvergenceWindow: 5}

	require.Equal(t, 0, dispatch([]string{"eventually"}, cfg, logger))
	require.Equal(t, 2, dispatch([]string{"eventually", "--window", "2"}, cfg, logger))
	require.Equal(t, 2, dispatch([]string{"bogus"}, cfg, logger))
}
