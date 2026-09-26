// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package dsl

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
	Hash:     "d1ce7303d199a28c881e0939f9ff41044ef6cfe968b1e4718b3f9e1997426f43",
}

func TestReaderGolden(t *testing.T) {
	goldentest.Check(t, "dsl", writeDSL(t, "mini.dsl", []byte(sampleDSL)), readerGolden)
}
