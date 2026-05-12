// trust.js — Trust Index tile (v0.12).
//
// Renders the 4th tile of the solo-KPI strip: a 0-100 composite Trust score
// with a band-coloured chip and breakdown of the 3 input components.
//
// Endpoint: GET /api/metrics/trust-index?days=28 → TrustResult
//
// Band → chip colour:
//   autonomous (≥80) → green
//   co-own     (60-79) → amber
//   copilot    (<60)   → red
//   no-data            → grey
//
// Empty state: shown when has_data=false (no task_attribution rows or no runs).

import { safeFetch } from './shared.js';

const ENDPOINT = '/api/metrics/trust-index?days=28';

// Band → CSS class on the .trust-band-chip element.
function bandClass(band) {
    switch (band) {
        case 'autonomous': return 'trust-band-autonomous';
        case 'co-own':     return 'trust-band-coown';
        case 'copilot':    return 'trust-band-copilot';
        default:           return 'trust-band-nodata';
    }
}

function bandLabel(band) {
    switch (band) {
        case 'autonomous': return 'Autonomous';
        case 'co-own':     return 'Co-own';
        case 'copilot':    return 'Copilot';
        default:           return 'No data';
    }
}

// renderTrustTile is the public entrypoint — fetch + render in place.
// Safe to call multiple times. No-op when the tile is absent from the DOM.
export async function renderTrustTile() {
    const tile = document.getElementById('solo-kpi-trust');
    if (!tile) return;

    const valueEl  = tile.querySelector('.solo-kpi-value');
    const chipEl   = tile.querySelector('.trust-band-chip');
    const breakdownEl = tile.querySelector('.trust-breakdown');
    const emptyEl  = tile.querySelector('.solo-kpi-empty');

    const res = await safeFetch(ENDPOINT);
    const data = res.data;

    if (!data || data.has_data === false) {
        if (valueEl) valueEl.textContent = '—';
        if (chipEl)  { chipEl.className = 'trust-band-chip trust-band-nodata'; chipEl.textContent = 'No data'; }
        if (breakdownEl) breakdownEl.textContent = '';
        if (emptyEl) emptyEl.hidden = false;
        return;
    }
    if (emptyEl) emptyEl.hidden = true;

    if (valueEl) valueEl.textContent = `${data.value}`;
    if (chipEl) {
        chipEl.className = `trust-band-chip ${bandClass(data.band)}`;
        chipEl.textContent = bandLabel(data.band);
    }
    if (breakdownEl) {
        const c = data.components || {};
        const acc  = (c.acceptance ?? 0) * 100;
        const cfr  = (c.ai_cfr ?? 0) * 100;
        const intv = c.intervention_rate ?? 0;
        breakdownEl.innerHTML =
            `<span title="Code Acceptance Rate (weight 40%)">Acc ${acc.toFixed(0)}%</span>` +
            ` · <span title="AI Change Failure Rate proxy (weight 35%)">CFR ${cfr.toFixed(0)}%</span>` +
            ` · <span title="Avg human interventions per run (weight 25%)">Intv ${intv.toFixed(2)}</span>`;
    }
}
