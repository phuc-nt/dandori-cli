//go:build server

package assignment

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DBHistoryProvider struct {
	pool  *pgxpool.Pool
	cache map[string]cacheEntry
	mu    sync.RWMutex
	ttl   time.Duration
}

type cacheEntry struct {
	stats     HistoryStats
	fetchedAt time.Time
}

func NewDBHistoryProvider(pool *pgxpool.Pool, ttl time.Duration) *DBHistoryProvider {
	if ttl == 0 {
		ttl = time.Hour
	}
	return &DBHistoryProvider{
		pool:  pool,
		cache: make(map[string]cacheEntry),
		ttl:   ttl,
	}
}

func (h *DBHistoryProvider) GetStats(agentName, issueType string) HistoryStats {
	key := agentName + ":" + issueType

	// Check cache
	h.mu.RLock()
	if entry, ok := h.cache[key]; ok {
		if time.Since(entry.fetchedAt) < h.ttl {
			h.mu.RUnlock()
			return entry.stats
		}
	}
	h.mu.RUnlock()

	// Query DB
	stats := h.queryStats(agentName, issueType)

	// Update cache
	h.mu.Lock()
	h.cache[key] = cacheEntry{stats: stats, fetchedAt: time.Now()}
	h.mu.Unlock()

	return stats
}

func (h *DBHistoryProvider) queryStats(agentName, issueType string) HistoryStats {
	if h.pool == nil {
		return HistoryStats{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var total, success int

	// Query runs for this agent on tasks of this issue type
	err := h.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE r.exit_code = 0) as success
		FROM runs r
		JOIN jira_tasks t ON r.jira_issue_key = t.issue_key
		WHERE r.agent_name = $1
		  AND t.issue_type = $2
		  AND r.status IN ('done', 'error')
	`, agentName, issueType).Scan(&total, &success)

	if err != nil || total == 0 {
		return HistoryStats{}
	}

	return HistoryStats{
		TotalRuns:   total,
		SuccessRuns: success,
		SuccessRate: float64(success) / float64(total),
	}
}

func (h *DBHistoryProvider) InvalidateCache(agentName, issueType string) {
	key := agentName + ":" + issueType
	h.mu.Lock()
	delete(h.cache, key)
	h.mu.Unlock()
}

func (h *DBHistoryProvider) ClearCache() {
	h.mu.Lock()
	h.cache = make(map[string]cacheEntry)
	h.mu.Unlock()
}
