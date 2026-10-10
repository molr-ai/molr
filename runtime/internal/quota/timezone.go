package quota

import (
	"sync"
	"time"
)

const timezoneCacheLimit = 64

type timezoneCache struct {
	mu        sync.RWMutex
	locations map[string]*time.Location
	order     []string
}

var periodTimezones timezoneCache

// Locations are immutable; period boundaries still depend on each call's time.
func (c *timezoneCache) load(name string) (*time.Location, error) {
	// Local may be replaced by the host; these names already have a cheap path.
	if name == "" || name == "UTC" || name == "Local" {
		return time.LoadLocation(name)
	}
	c.mu.RLock()
	location := c.locations[name]
	c.mu.RUnlock()
	if location != nil {
		return location, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if location = c.locations[name]; location != nil {
		return location, nil
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	if c.locations == nil {
		c.locations = make(map[string]*time.Location)
	}
	if len(c.order) == timezoneCacheLimit {
		delete(c.locations, c.order[0])
		copy(c.order, c.order[1:])
		c.order[len(c.order)-1] = name
	} else {
		c.order = append(c.order, name)
	}
	c.locations[name] = location
	return location, nil
}
