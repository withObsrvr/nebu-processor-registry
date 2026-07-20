package validatoridentityenricher

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"
)

type cacheEntry struct {
	key       string
	result    LookupResult
	expiresAt time.Time
}

// CacheOptions bound identity caching for long-running streams. Resolved and
// not-found entries live for the process lifetime. Unavailable entries use a
// short TTL so outages do not cause one HTTP request per ledger.
type CacheOptions struct {
	MaxEntries     int
	UnavailableTTL time.Duration
	Now            func() time.Time
}

// CachedResolver adds a bounded LRU cache to another Resolver.
type CachedResolver struct {
	delegate       Resolver
	maxEntries     int
	unavailableTTL time.Duration
	now            func() time.Time

	mu      sync.Mutex
	entries map[string]*list.Element
	lru     *list.List
}

// NewCachedResolver constructs a process-local identity cache.
func NewCachedResolver(delegate Resolver, opts CacheOptions) (*CachedResolver, error) {
	if delegate == nil {
		return nil, fmt.Errorf("resolver is required")
	}
	if opts.MaxEntries <= 0 {
		return nil, fmt.Errorf("cache size must be positive")
	}
	if opts.UnavailableTTL < 0 {
		return nil, fmt.Errorf("unavailable cache TTL cannot be negative")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &CachedResolver{
		delegate:       delegate,
		maxEntries:     opts.MaxEntries,
		unavailableTTL: opts.UnavailableTTL,
		now:            opts.Now,
		entries:        make(map[string]*list.Element),
		lru:            list.New(),
	}, nil
}

func (c *CachedResolver) Source() string        { return c.delegate.Source() }
func (c *CachedResolver) TemporalBasis() string { return c.delegate.TemporalBasis() }

func (c *CachedResolver) Resolve(ctx context.Context, networkName, publicKey string) LookupResult {
	key := networkName + "\x00" + publicKey
	if result, ok := c.get(key); ok {
		return result
	}
	result := c.delegate.Resolve(ctx, networkName, publicKey)
	c.put(key, result)
	return result
}

func (c *CachedResolver) get(key string) (LookupResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return LookupResult{}, false
	}
	entry := element.Value.(cacheEntry)
	if !entry.expiresAt.IsZero() && !c.now().Before(entry.expiresAt) {
		c.lru.Remove(element)
		delete(c.entries, key)
		return LookupResult{}, false
	}
	c.lru.MoveToFront(element)
	return entry.result, true
}

func (c *CachedResolver) put(key string, result LookupResult) {
	expiresAt := time.Time{}
	if result.Status == StatusUnavailable {
		if c.unavailableTTL == 0 {
			return
		}
		expiresAt = c.now().Add(c.unavailableTTL)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.entries[key]; ok {
		existing.Value = cacheEntry{key: key, result: result, expiresAt: expiresAt}
		c.lru.MoveToFront(existing)
		return
	}
	element := c.lru.PushFront(cacheEntry{key: key, result: result, expiresAt: expiresAt})
	c.entries[key] = element
	if c.lru.Len() <= c.maxEntries {
		return
	}
	oldest := c.lru.Back()
	if oldest != nil {
		c.lru.Remove(oldest)
		delete(c.entries, oldest.Value.(cacheEntry).key)
	}
}
