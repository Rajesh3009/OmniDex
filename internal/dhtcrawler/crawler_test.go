package dhtcrawler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
)

func TestBoundedCacheRingBuffer(t *testing.T) {
	cacheSize := 5
	cache := newBoundedCache(cacheSize)

	// Add 5 items
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key_%d", i)
		if cache.HasOrAdd(key) {
			t.Fatalf("expected key %s to not exist", key)
		}
	}

	// Verify all 5 exist
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key_%d", i)
		if !cache.HasOrAdd(key) {
			t.Fatalf("expected key %s to exist", key)
		}
	}

	// Add 6th item, which should evict key_0 (FIFO)
	if cache.HasOrAdd("key_5") {
		t.Fatalf("expected key_5 to not exist")
	}

	// key_0 must be evicted
	if cache.HasOrAdd("key_0") {
		t.Fatalf("expected key_0 to have been evicted")
	}

	// key_1, key_2, key_3, key_4, key_5 must exist (note: checking key_0 re-added it, evicting key_1!)
	if cache.HasOrAdd("key_1") {
		t.Fatalf("expected key_1 to have been evicted after key_0 re-added")
	}
}

func TestBoundedCacheConcurrent(t *testing.T) {
	cache := newBoundedCache(100)
	var wg sync.WaitGroup

	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				key := fmt.Sprintf("k_%d_%d", gid, i%50)
				cache.HasOrAdd(key)
			}
		}(g)
	}
	wg.Wait()
}

func TestMetricsDataRace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &Crawler{
		ctx:      ctx,
		cancel:   cancel,
		visited:  newBoundedCache(100),
		workChan: make(chan AnnounceJob, 100),
	}

	var wg sync.WaitGroup
	// Mutators doing atomic ops
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				atomic.AddInt64(&c.metrics.AnnouncesReceived, 1)
				atomic.AddInt64(&c.metrics.MetadataResolved, 1)
				atomic.AddInt64(&c.metrics.ActiveResolving, 1)
				atomic.AddInt64(&c.metrics.ActiveResolving, -1)
				atomic.AddInt64(&c.metrics.MetadataFailed, 1)
			}
		}()
	}

	// Reader calling Metrics()
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				m := c.Metrics()
				if m.AnnouncesReceived < 0 {
					t.Errorf("unexpected negative metric")
				}
			}
		}()
	}

	wg.Wait()
}

func TestEnqueueWithTrackersVisitedFastPath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &Crawler{
		ctx:      ctx,
		cancel:   cancel,
		visited:  newBoundedCache(100),
		workChan: make(chan AnnounceJob, 10),
	}

	var hash metainfo.Hash
	hash[0] = 0xAB

	// First enqueue should succeed
	c.enqueueWithTrackers(hash, nil, 0, nil)
	if len(c.workChan) != 1 {
		t.Fatalf("expected 1 item in workChan, got %d", len(c.workChan))
	}

	// Second enqueue of identical hash must hit visited fast-path and NOT enqueue again
	c.enqueueWithTrackers(hash, nil, 0, nil)
	if len(c.workChan) != 1 {
		t.Fatalf("expected still 1 item in workChan, got %d", len(c.workChan))
	}
}

func TestEnqueueWithTrackersMonthlyLimitFastPath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &Crawler{
		ctx:      ctx,
		cancel:   cancel,
		visited:  newBoundedCache(100),
		workChan: make(chan AnnounceJob, 10),
	}
	c.monthlyLimitExceeded.Store(true)

	var hash metainfo.Hash
	hash[0] = 0xCD

	c.enqueueWithTrackers(hash, nil, 0, nil)
	if len(c.workChan) != 0 {
		t.Fatalf("expected 0 items in workChan due to monthly limit, got %d", len(c.workChan))
	}
}

func TestWorkerExitRaceOnWorkChan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	c := &Crawler{
		ctx:      ctx,
		cancel:   cancel,
		visited:  newBoundedCache(100),
		workChan: make(chan AnnounceJob, 10),
	}

	// Cancel context first
	cancel()

	// Fill channel with jobs
	var hash metainfo.Hash
	c.workChan <- AnnounceJob{Hash: hash}

	// Worker should immediately return on c.ctx.Err() != nil without panicking or resolving
	c.wg.Add(1)
	done := make(chan struct{})
	go func() {
		c.worker(0)
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatalf("worker did not exit cleanly when context was canceled")
	}
}
