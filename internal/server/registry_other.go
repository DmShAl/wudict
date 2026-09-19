// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !windows

package server

// Everywhere else a rename over an open file is legal, so there is nothing to
// hand back: the entry keeps serving its prepared view for the length of the
// rebuild, exactly as before. See registry_windows.go for why it does not.
func releasePrepared(e *entry, textDB string) {}
