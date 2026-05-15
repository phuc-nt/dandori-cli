// repo-filter.js — multi-repo dropdown (v0.14).
//
// Fetches /api/metrics/repos. Renders <select id="repo-filter"> with an
// "All repos" entry plus one row per active repo, ordered desc by merged
// count. Hides the entire row when < 2 repos exist (solo-repo users).
//
// On change, dispatches a `dandori:repo-change` CustomEvent on document
// with { detail: { repo } } so trust.js + pr-cycle.js can refetch.
//
// State persists across tab switches via window.__dandoriRepoFilter so
// late-bound widgets can read the current scope on initial render.

import { safeFetch } from './shared.js';

const REPOS_ENDPOINT = '/api/metrics/repos?days=28';

export function getCurrentRepoFilter() {
    return window.__dandoriRepoFilter || '';
}

export async function renderRepoFilter() {
    const row = document.getElementById('repo-filter-row');
    const select = document.getElementById('repo-filter');
    if (!row || !select) return;

    const res = await safeFetch(REPOS_ENDPOINT);
    const repos = Array.isArray(res.data) ? res.data : [];

    if (repos.length < 2) {
        row.hidden = true;
        return;
    }

    // Idempotent populate — clear any prior options first.
    select.innerHTML = '';
    const all = document.createElement('option');
    all.value = '';
    all.textContent = `All repos (${repos.length})`;
    select.appendChild(all);
    for (const r of repos) {
        const opt = document.createElement('option');
        opt.value = r.repo;
        opt.textContent = `${r.repo} · ${r.merged_count} merged`;
        select.appendChild(opt);
    }
    row.hidden = false;

    // Restore prior selection if still present in the option set.
    const prior = getCurrentRepoFilter();
    if (prior && [...select.options].some(o => o.value === prior)) {
        select.value = prior;
    }

    select.addEventListener('change', () => {
        const repo = select.value || '';
        window.__dandoriRepoFilter = repo;
        document.dispatchEvent(new CustomEvent('dandori:repo-change', {
            detail: { repo },
        }));
    });
}
