// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package server

import (
	"os"
	"runtime"
	"syscall"
)

// peakRSSMB reads the child's peak resident set from rusage.
func peakRSSMB(ps *os.ProcessState) float64 {
	ru, ok := ps.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0
	}
	return maxRSSBytes(ru.Maxrss) / (1 << 20)
}

// maxRSSBytes normalises rusage.Maxrss, which is bytes on Darwin and
// kilobytes on Linux.
func maxRSSBytes(v int64) float64 {
	if runtime.GOOS == "darwin" {
		return float64(v)
	}
	return float64(v) * 1024
}
