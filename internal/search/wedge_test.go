// Copyright (C) 2026 glowinthedark
//
// SPDX-License-Identifier: GPL-3.0-or-later

package search

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/wuweidict/wudict/internal/dict"
)

// wedgedDict answers Exact by announcing itself and then never returning.
// It is the backend the pre-open checks cannot see: the context is live at
// query time, and the query does not finish.
type wedgedDict struct {
	started chan struct{}
	release chan struct{}
}

func (w wedgedDict) Meta() dict.Meta { return dict.Meta{Name: "wedged", Path: "/x/wedged.mdx"} }
func (w wedgedDict) Caps() dict.Caps { return dict.Caps{Exact: true, Prefix: true} }
func (w wedgedDict) Exact(string, int) ([]dict.Result, error) {
	w.started <- struct{}{}
	<-w.release
	return nil, nil
}
func (w wedgedDict) Prefix(string, int) ([]dict.Result, error) { return nil, nil }
func (w wedgedDict) Keywords(int, int) []string                { return nil }
func (w wedgedDict) Resource(string) (io.ReadCloser, string, error) {
	return nil, "", dict.ErrNotFound
}
func (w wedgedDict) Close() error { return nil }

// A query that never finishes must not hold the fan-out: cancelling while it
// runs ends the request with an error in that slot, and the abandoned query
// drains when it is released.
//
// Only the wedged slot is asserted. Whether the healthy slot lands its result
// or reports the cancellation is a scheduler race this test cannot pin - the
// cancel and the healthy query's send compete, and either outcome is honest.
// StreamOpen's test pins the finished-result case properly, through its emit.
func TestAllAbandonsAWedgedQueryOnCancel(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	wedged := wedgedDict{started: started, release: release}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan []Hit, 1)
	go func() { done <- All(ctx, []dict.Dictionary{okDict{}, wedged}, Exact, "w", 5) }()
	<-started
	cancel()
	select {
	case hits := <-done:
		if !errors.Is(hits[1].Err, context.Canceled) {
			t.Fatalf("wedged slot = %+v, want a cancellation", hits[1])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("All stayed wedged behind a query that cannot finish")
	}
	close(release)
}

func TestStreamOpenAbandonsAWedgedQueryOnCancel(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	wedged := wedgedDict{started: started, release: release}

	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var wedgedHit Hit
	haveWedgedHit := false
	done := make(chan struct{})
	go func() {
		defer close(done)
		StreamOpen(ctx, []Opener{
			func() (dict.Dictionary, error) { return okDict{}, nil },
			func() (dict.Dictionary, error) { return wedged, nil },
		}, Exact, "w", 5, func(i int, h Hit) {
			if i == 1 {
				mu.Lock()
				wedgedHit, haveWedgedHit = h, true
				mu.Unlock()
			}
		})
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StreamOpen stayed wedged behind a query that cannot finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if !haveWedgedHit || !errors.Is(wedgedHit.Err, context.Canceled) {
		t.Fatalf("wedged slot = %+v, want a cancellation", wedgedHit)
	}
	close(release)
}
