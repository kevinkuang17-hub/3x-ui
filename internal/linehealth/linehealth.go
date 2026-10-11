// Package linehealth holds which inbounds the line-health probe currently considers unreachable,
// so subscription renderers can leave them out until they recover.
package linehealth

import (
	"sort"
	"sync"
)

var (
	mu   sync.RWMutex
	down = map[int]struct{}{}
)

// IsDown reports whether the inbound is currently hidden from subscriptions.
func IsDown(inboundID int) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := down[inboundID]
	return ok
}

// Set marks the inbound down or up and reports whether that changed its state.
func Set(inboundID int, isDown bool) bool {
	mu.Lock()
	defer mu.Unlock()
	_, was := down[inboundID]
	if was == isDown {
		return false
	}
	if isDown {
		down[inboundID] = struct{}{}
	} else {
		delete(down, inboundID)
	}
	return true
}

// Down lists the inbound ids currently hidden, sorted.
func Down() []int {
	mu.RLock()
	defer mu.RUnlock()
	ids := make([]int, 0, len(down))
	for id := range down {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// Reset clears every mark (feature turned off, or tests).
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	down = map[int]struct{}{}
}
