// solo-kpi.js — Solo engineer KPI hero strip (v0.11.2, Fix A).
//
// Renders 3 tiles at the top of the Engineering view (above sub-tabs):
//   1. Success Rate (7d) — current %, WoW delta, mini sparkline
//   2. Rework Rate (7d)  — current %, WoW delta, mini sparkline
//   3. Avg Cost / Run (7d) — current $, WoW delta, mini sparkline
//
// Sparklines are inline SVG paths (no Chart.js — keeps initial load light).
// Delta badge direction: success-rate up = good, rework-rate up = bad, cost up = bad.
// Empty state: if a tile has no data, shows a placeholder message.

import { safeFetch } from './shared.js';

// Metric config encodes which direction is "good" for delta coloring.
// higherIsBetter: true  → positive delta = green (success rate)
// higherIsBetter: false → positive delta = red (rework rate, cost)
const METRICS = [
    {
        id: 'solo-kpi-success',
        label: 'Success Rate · 7d',
        endpoint: '/api/trends/success-rate?days=56&window=7d',
        format: v => `${v.toFixed(1)}%`,
        higherIsBetter: true,
    },
    {
        id: 'solo-kpi-rework',
        label: 'Rework Rate · 7d',
        endpoint: '/api/trends/rework-rate?days=56&window=7d',
        format: v => `${v.toFixed(1)}%`,
        higherIsBetter: false,
    },
    {
        id: 'solo-kpi-cost',
        label: 'Avg Cost / Run · 7d',
        endpoint: '/api/trends/cost-per-run?days=56&window=7d',
        format: v => `$${v.toFixed(3)}`,
        higherIsBetter: false,
    },
];

// buildSparklinePath converts an array of numeric values to an SVG polyline
// path string within a 120×32 viewBox. Returns null when fewer than 2 points.
function buildSparklinePath(values) {
    const pts = values.filter(v => v !== null && v !== undefined && isFinite(v));
    if (pts.length < 2) return null;
    const minV = Math.min(...pts);
    const maxV = Math.max(...pts);
    const rangeV = maxV - minV || 1;
    const W = 120, H = 32, pad = 2;
    const coords = pts.map((v, i) => {
        const x = pad + (i / (pts.length - 1)) * (W - 2 * pad);
        const y = H - pad - ((v - minV) / rangeV) * (H - 2 * pad);
        return `${x.toFixed(1)},${y.toFixed(1)}`;
    });
    return `M ${coords.join(' L ')}`;
}

// wowDelta returns { delta, prevValue, currValue } from the last 2 weekly
// data points in the trend response. Returns null when insufficient data.
function wowDelta(points) {
    if (!Array.isArray(points)) return null;
    const valid = points.filter(p => p.has_data !== false && p.value !== null && p.value !== undefined);
    if (valid.length < 2) return null;
    const curr = valid[valid.length - 1].value;
    const prev = valid[valid.length - 2].value;
    return { delta: curr - prev, curr, prev };
}

// renderTile populates a single .solo-kpi-tile element with live data.
function renderTile(metric, points) {
    const tile = document.getElementById(metric.id);
    if (!tile) return;

    const valueEl  = tile.querySelector('.solo-kpi-value');
    const deltaEl  = tile.querySelector('.solo-kpi-delta');
    const sparkEl  = tile.querySelector('.solo-kpi-spark');
    const emptyEl  = tile.querySelector('.solo-kpi-empty');

    const valid = Array.isArray(points) ? points.filter(p => p.has_data !== false) : [];

    if (valid.length === 0) {
        if (valueEl) valueEl.textContent = '—';
        if (deltaEl) deltaEl.textContent = '';
        if (sparkEl) sparkEl.style.display = 'none';
        if (emptyEl) emptyEl.hidden = false;
        return;
    }

    // Hide empty state placeholder.
    if (emptyEl) emptyEl.hidden = true;

    // Current value = last valid point.
    const curr = valid[valid.length - 1].value;
    if (valueEl) valueEl.textContent = metric.format(curr);

    // WoW delta badge.
    const wow = wowDelta(valid);
    if (deltaEl && wow) {
        const sign  = wow.delta >= 0 ? '+' : '';
        const good  = metric.higherIsBetter ? wow.delta >= 0 : wow.delta <= 0;
        const flat  = Math.abs(wow.delta) < 0.05;
        const cls   = flat ? 'flat' : good ? 'good' : 'bad';
        const arrow = flat ? '' : (wow.delta > 0 ? '↑' : '↓');
        const fmtDelta = metric.id === 'solo-kpi-cost'
            ? `${sign}$${Math.abs(wow.delta).toFixed(3)}`
            : `${sign}${wow.delta.toFixed(1)}pp`;
        deltaEl.textContent = `${arrow} ${fmtDelta} WoW`;
        deltaEl.className = `solo-kpi-delta ${cls}`;
    } else if (deltaEl) {
        deltaEl.textContent = '—';
        deltaEl.className = 'solo-kpi-delta flat';
    }

    // Sparkline (inline SVG, no Chart.js).
    if (sparkEl) {
        const path = buildSparklinePath(valid.map(p => p.value));
        if (path) {
            sparkEl.style.display = '';
            sparkEl.innerHTML = `<path d="${path}" />`;
        } else {
            sparkEl.style.display = 'none';
        }
    }
}

// renderSoloKpiStrip fetches the 3 trend endpoints in parallel and populates
// the tiles in the #solo-kpi-strip container. Safe to call multiple times
// (re-renders in place). No-op when the container is absent from the DOM.
export async function renderSoloKpiStrip() {
    const strip = document.getElementById('solo-kpi-strip');
    if (!strip) return;

    // Fetch all 3 endpoints in parallel.
    const results = await Promise.all(
        METRICS.map(m => safeFetch(m.endpoint))
    );

    results.forEach((res, i) => {
        const metric = METRICS[i];
        // res.data may be { data: [...] } (nested) or directly an array
        // depending on which endpoint shape is returned.
        const points = Array.isArray(res.data)
            ? res.data
            : (Array.isArray(res.data?.data) ? res.data.data : []);
        renderTile(metric, points);
    });
}
