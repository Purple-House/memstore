package memstore

import (
	"container/heap"
	"context"
	"log"
	"time"
)

// agentExpiryItem is a single entry in the min-heap.
type agentExpiryItem struct {
	AgentDomain string
	Region      string
	ExpiresAt   time.Time
}

// agentExpiryHeap is a min-heap ordered by ExpiresAt (earliest first).
type agentExpiryHeap []*agentExpiryItem

func (h agentExpiryHeap) Len() int           { return len(h) }
func (h agentExpiryHeap) Less(i, j int) bool { return h[i].ExpiresAt.Before(h[j].ExpiresAt) }
func (h agentExpiryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *agentExpiryHeap) Push(x any) {
	*h = append(*h, x.(*agentExpiryItem))
}

func (h *agentExpiryHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // prevent memory leak
	*h = old[:n-1]
	return item
}

// Peek returns the earliest-expiring entry without removing it.
func (h agentExpiryHeap) Peek() *agentExpiryItem {
	if len(h) == 0 {
		return nil
	}
	return h[0]
}

// WALDeleter is implemented by WALer to persist agent deletions.
// Defined here to avoid a circular import between memstore and wal packages.
type WALDeleter interface {
	AppendDeleteAgent(region, agentDomain string) error
}

// SetTTL sets the agent time-to-live. Must be called before StartExpiryWorker.
func (mem *MemStore) SetTTL(ttl time.Duration) {
	mem.agentTTL = ttl
}

func (mem *MemStore) pushExpiry(agentDomain, region string, expiresAt time.Time) {
	if mem.agentTTL == 0 {
		return
	}
	mem.expiryMu.Lock()
	heap.Push(mem.expiryHeap, &agentExpiryItem{
		AgentDomain: agentDomain,
		Region:      region,
		ExpiresAt:   expiresAt,
	})
	mem.expiryMu.Unlock()

	// Signal the worker: the new entry may be earlier than the current timer target.
	select {
	case mem.expiryReset <- struct{}{}:
	default:
	}
}

func (mem *MemStore) StartExpiryWorker(ctx context.Context, waler WALDeleter) {
	go func() {
		var timer *time.Timer
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()

		for {
			mem.expiryMu.Lock()
			top := mem.expiryHeap.Peek()
			mem.expiryMu.Unlock()

			if top == nil {
				// Nothing scheduled — park until a new entry arrives or shutdown.
				select {
				case <-mem.expiryReset:
					continue
				case <-ctx.Done():
					return
				}
			}

			delay := time.Until(top.ExpiresAt)
			if timer == nil {
				timer = time.NewTimer(delay)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(delay)
			}

			select {
			case <-timer.C:
				mem.processExpired(waler)
			case <-mem.expiryReset:
				// A new entry may have shifted the heap top earlier; re-arm timer.
				continue
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (mem *MemStore) processExpired(waler WALDeleter) {
	now := time.Now()
	for {
		mem.expiryMu.Lock()
		top := mem.expiryHeap.Peek()
		if top == nil || top.ExpiresAt.After(now) {
			mem.expiryMu.Unlock()
			break
		}
		item := heap.Pop(mem.expiryHeap).(*agentExpiryItem)
		mem.expiryMu.Unlock()

		// Check the agent's actual LastSeenAt under a read lock.
		data := mem.RegionExist(item.Region)
		data.Mu.RLock()
		agent, ok := data.Agents[item.AgentDomain]
		if !ok {
			data.Mu.RUnlock()
			// Already deleted via another path; skip.
			continue
		}
		lastSeenAt := agent.LastSeenAt
		data.Mu.RUnlock()

		if now.Sub(time.Unix(lastSeenAt, 0)) < mem.agentTTL {
			// Agent re-registered since this heap entry was pushed; stale entry — discard.
			continue
		}

		// Truly expired: persist to WAL first so the deletion survives a crash.
		if err := waler.AppendDeleteAgent(item.Region, item.AgentDomain); err != nil {
			log.Printf("[expiry] WAL delete failed for agent %s region %s: %v — retry in 5m",
				item.AgentDomain, item.Region, err)
			// Re-enqueue with a short retry delay rather than silently losing the deletion.
			mem.pushExpiry(item.AgentDomain, item.Region, now.Add(5*time.Minute))
			continue
		}

		mem.DeleteAgent(item.Region, item.AgentDomain)
		log.Printf("[expiry] evicted stale agent %s in region %s (last seen: %s)",
			item.AgentDomain, item.Region, time.Unix(lastSeenAt, 0).Format(time.RFC3339))
	}
}
