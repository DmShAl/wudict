// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package server

import "os"

// peakRSSMB is 0 on Windows: SysUsage there carries CPU times only, no peak
// working set, so the sweep's peak RSS column reads 0.0 MB.
func peakRSSMB(*os.ProcessState) float64 { return 0 }
