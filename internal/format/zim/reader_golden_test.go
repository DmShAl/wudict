// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package zim

import (
	"testing"

	"github.com/wuweidict/wudict/internal/store/goldentest"
)

// The prepared content of this package's fixture, pinned. A failure here
// means what an ingest writes has changed: bump ReaderVersion (or the version
// of whichever layer changed) so existing libraries are told to rebuild, then
// update the golden to the value the failure prints.
var readerGolden = goldentest.Golden{
	Versions: "reader=1 ingest=1 markup=2 fold=1",
	Hash:     "6c39aa99968f0706abe6df0b91305cc8459c71a9dc8b97b1525c6b66f56ffa45",
}

func TestReaderGolden(t *testing.T) {
	goldentest.Check(t, "zim", writeZIM(t, sample()), readerGolden)
}
