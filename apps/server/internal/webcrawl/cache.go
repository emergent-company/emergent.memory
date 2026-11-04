package webcrawl

import "container/list"

// bodyCache is a bounded in-memory LRU of response bodies keyed by canonical
// URL. It exists so the caller (domain/sources) can hand fetched bodies to the
// ingestion pipeline without a second GET: bodies are kept up to the byte
// budget and evicted least-recently-used when the budget is exceeded. A body
// larger than the budget is never stored.
type bodyCache struct {
	maxBytes int
	bytes    int
	entries  map[string]*bodyCacheEntry
	ll       *list.List // front = most recently used
}

type bodyCacheEntry struct {
	key  string
	body []byte
	elem *list.Element
}

func newBodyCache(maxBytes int) *bodyCache {
	return &bodyCache{
		maxBytes: maxBytes,
		entries:  map[string]*bodyCacheEntry{},
		ll:       list.New(),
	}
}

func (c *bodyCache) get(key string) []byte {
	if e, ok := c.entries[key]; ok {
		c.ll.MoveToFront(e.elem)
		return e.body
	}
	return nil
}

func (c *bodyCache) put(key string, body []byte) {
	if c.maxBytes <= 0 || len(body) > c.maxBytes {
		return // too large for the cache: never stored
	}
	if e, ok := c.entries[key]; ok {
		// Update in place.
		c.bytes += len(body) - len(e.body)
		e.body = body
		c.ll.MoveToFront(e.elem)
	} else {
		e := &bodyCacheEntry{key: key, body: body}
		e.elem = c.ll.PushFront(e)
		c.entries[key] = e
		c.bytes += len(body)
	}
	// Evict least-recently-used until within budget.
	for c.bytes > c.maxBytes {
		back := c.ll.Back()
		if back == nil {
			break
		}
		e := back.Value.(*bodyCacheEntry)
		c.ll.Remove(back)
		delete(c.entries, e.key)
		c.bytes -= len(e.body)
	}
}
