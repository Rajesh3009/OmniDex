package scraper

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Registry manages the set of available source scrapers and provides
// concurrent multi-source search orchestration.
type Registry struct {
	mu       sync.RWMutex
	scrapers []SourceScraper
	byName   map[string]SourceScraper
}

// NewRegistry initializes an empty scraper registry.
func NewRegistry() *Registry {
	return &Registry{
		scrapers: make([]SourceScraper, 0, 8),
		byName:   make(map[string]SourceScraper, 8),
	}
}

// DefaultRegistry creates and returns a Registry pre-populated with all 8 native Go scrapers:
// YTS, Nyaa.si, Sukebei, TorrentDownloads, The Pirate Bay, AudioBookBay, EZTV, and OxTorrent.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(NewYTSScraper())
	r.Register(NewNyaaScraper())
	r.Register(NewSukebeiScraper())
	r.Register(NewTorrentDownloadsScraper())
	r.Register(NewThePirateBayScraper())
	r.Register(NewAudioBookBayScraper())
	r.Register(NewEZTVScraper())
	r.Register(NewOxTorrentScraper())
	return r
}

// Register adds a SourceScraper to the registry. If a scraper with the same
// name (case-insensitive) already exists, it is replaced.
func (r *Registry) Register(s SourceScraper) {
	if s == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	key := strings.ToLower(strings.TrimSpace(s.Name()))
	if key == "" {
		return
	}

	if existing, exists := r.byName[key]; exists {
		// Replace in slice
		for i, item := range r.scrapers {
			if strings.EqualFold(item.Name(), existing.Name()) {
				r.scrapers[i] = s
				break
			}
		}
	} else {
		r.scrapers = append(r.scrapers, s)
	}

	r.byName[key] = s
}

// Get returns the SourceScraper registered with the given name (case-insensitive).
func (r *Registry) Get(name string) (SourceScraper, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, exists := r.byName[strings.ToLower(strings.TrimSpace(name))]
	return s, exists
}

// All returns a slice copy of all registered SourceScrapers.
func (r *Registry) All() []SourceScraper {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]SourceScraper, len(r.scrapers))
	copy(result, r.scrapers)
	return result
}

// SearchAll queries all registered scrapers concurrently, aggregates their results,
// and gracefully tolerates individual source failures.
func (r *Registry) SearchAll(ctx context.Context, query string, opts ...Option) ([]Torrent, error) {
	scrapers := r.All()
	if len(scrapers) == 0 {
		return []Torrent{}, nil
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	var aggregated []Torrent
	var errs []string

	for _, s := range scrapers {
		wg.Add(1)
		go func(scraper SourceScraper) {
			defer wg.Done()

			// Check context before executing
			select {
			case <-ctx.Done():
				return
			default:
			}

			torrents, err := scraper.Search(ctx, query, opts...)
			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", scraper.Name(), err))
				return
			}
			if len(torrents) > 0 {
				aggregated = append(aggregated, torrents...)
			}
		}(s)
	}

	wg.Wait()

	// If context was cancelled, report context error
	if ctx.Err() != nil {
		return aggregated, ctx.Err()
	}

	// If all scrapers failed and no results were found, return aggregate error
	if len(aggregated) == 0 && len(errs) == len(scrapers) {
		return nil, fmt.Errorf("all scrapers failed: %s", strings.Join(errs, "; "))
	}

	return aggregated, nil
}
