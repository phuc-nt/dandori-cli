// pr-cycle.js — PR Review Cycle Time tile (v0.13).
//
// Renders the 5th tile of the solo-KPI strip: median (p50) + p75 hours
// from PR submission to first APPROVED review. Diagnostic only — used
// by framework §8 to disambiguate Trust+Deploy quadrants.
//
// Endpoint: GET /api/metrics/pr-cycle-time?days=28 → PRCycleResult
//
// Empty state: has_data=false when no merged PRs OR none had an approving
// review (solo engineer / auto-merge teams).

import { safeFetch } from './shared.js';
import { getCurrentRepoFilter } from './repo-filter.js';

const ENDPOINT_BASE = '/api/metrics/pr-cycle-time?days=28';

function endpointFor(repo) {
    return repo ? `${ENDPOINT_BASE}&repo=${encodeURIComponent(repo)}` : ENDPOINT_BASE;
}

// Format hours into a compact label: "4.2h", "1.8d" past 48h.
function formatHours(h) {
    if (!Number.isFinite(h) || h < 0) return '—';
    if (h < 48) return `${h.toFixed(1)}h`;
    return `${(h / 24).toFixed(1)}d`;
}

export async function renderPRCycleTile() {
    const tile = document.getElementById('solo-kpi-pr-cycle');
    if (!tile) return;

    const valueEl     = tile.querySelector('.solo-kpi-value');
    const secondaryEl = tile.querySelector('.pr-cycle-secondary');
    const breakdownEl = tile.querySelector('.pr-cycle-breakdown');
    const emptyEl     = tile.querySelector('.solo-kpi-empty');

    const repo = getCurrentRepoFilter();
    const res = await safeFetch(endpointFor(repo));
    const data = res.data;

    if (!data || data.has_data === false) {
        if (valueEl)     valueEl.textContent = '—';
        if (secondaryEl) secondaryEl.textContent = '';
        if (breakdownEl) {
            // Distinguish "no merged PRs" vs "merged but no reviews" — the
            // latter is the meaningful solo/auto-merge signal.
            const total = data?.merged_total ?? 0;
            breakdownEl.textContent = total > 0
                ? `${total} merged · 0 reviewed`
                : '';
        }
        if (emptyEl) emptyEl.hidden = false;
        return;
    }
    if (emptyEl) emptyEl.hidden = true;

    if (valueEl)     valueEl.textContent = formatHours(data.median_hours);
    if (secondaryEl) secondaryEl.textContent = `p75 ${formatHours(data.p75_hours)}`;
    if (breakdownEl) {
        let line = `${data.with_approval} / ${data.merged_total} PRs reviewed`;
        if (data.has_lines_data) {
            line += ` · ~${data.median_lines_changed} LoC/PR`;
        }
        breakdownEl.textContent = line;
    }
}

document.addEventListener('dandori:repo-change', () => { renderPRCycleTile(); });
