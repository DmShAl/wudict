// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package slob

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
	Hash:     "ba7274bf41e8766869aa804973945b6c46293338f7385555b71b496fa35b6121",
}

func TestReaderGolden(t *testing.T) {
	goldentest.Check(t, "slob", buildSlob(t), readerGolden)
}
