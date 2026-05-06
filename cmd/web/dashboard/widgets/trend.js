// trend.js — Week-over-week trend line chart widget (v0.11 Phase 03).
//
// Renders under Engineering → Cache & Cost sub-tab.
// Fetches 3 series in parallel:
//   /api/trends/success-rate?days=84   (12 weeks)
//   /api/trends/cost?days=84
//   /api/trends/rework-rate?days=84
//
// Chart.js line chart: x = week_start ISO, y = value, nulls for gap weeks.
// Slope labels shown as card-sub text below each series name.
// Graceful empty state when all series have no data.

import { safeFetch, escapeHtml } from './shared.js';

const Chart = window.Chart;

let trendChartInstance = null;

// slope computes least-squares slope over HasData=true points.
// Returns 0 when < 2 data points.
function slope(points) {
    const pts = points
        .map((p, i) => ({ x: i, y: p.value, has: p.has_data }))
        .filter(p => p.has);
    const n = pts.length;
    if (n < 2) return 0;
    let sx = 0, sy = 0, sxy = 0, sx2 = 0;
    pts.forEach(p => { sx += p.x; sy += p.y; sxy += p.x * p.y; sx2 += p.x * p.x; });
    const denom = n * sx2 - sx * sx;
    if (Math.abs(denom) < 1e-9) return 0;
    return (n * sxy - sx * sy) / denom;
}

function slopeLabel(s, n) {
    if (n < 3) return 'insufficient data';
    const FLAT = 0.5;
    if (Math.abs(s) < FLAT) return 'flat';
    if (s > 0) return `improving ${s.toFixed(1)} pp/week`;
    return `declining ${(-s).toFixed(1)} pp/week`;
}

function dataPointCount(points) {
    return points.filter(p => p.has_data).length;
}

export async function renderTrendChart() {
    const canvas = document.getElementById('eng-trend-canvas');
    const empty  = document.getElementById('eng-trend-empty');
    const sub    = document.getElementById('eng-trend-sub');
    if (!canvas) return;

    const [srRes, costRes, rwRes] = await Promise.all([
        safeFetch('/api/trends/success-rate?days=84'),
        safeFetch('/api/trends/cost?days=84'),
        safeFetch('/api/trends/rework-rate?days=84'),
    ]);

    const srPts   = Array.isArray(srRes.data)   ? srRes.data   : [];
    const costPts = Array.isArray(costRes.data)  ? costRes.data : [];
    const rwPts   = Array.isArray(rwRes.data)    ? rwRes.data   : [];

    const totalData = dataPointCount(srPts) + dataPointCount(costPts) + dataPointCount(rwPts);

    if (totalData === 0) {
        canvas.style.display = 'none';
        if (empty) empty.hidden = false;
        return;
    }
    canvas.style.display = '';
    if (empty) empty.hidden = true;

    // Use the longest series for x-axis labels.
    const longest = [srPts, costPts, rwPts].reduce((a, b) => (b.length > a.length ? b : a), []);
    const labels = longest.map(p => p.week_start);

    // Convert to Chart.js null-for-gap arrays.
    const toValues = pts => {
        const byWeek = {};
        pts.forEach(p => { if (p.has_data) byWeek[p.week_start] = p.value; });
        return labels.map(l => (byWeek[l] !== undefined ? byWeek[l] : null));
    };

    // Compute slope labels.
    const srSlope   = slope(srPts);
    const costSlope = slope(costPts);
    const rwSlope   = slope(rwPts);
    const srN   = dataPointCount(srPts);
    const costN = dataPointCount(costPts);
    const rwN   = dataPointCount(rwPts);

    const slopeInfo = [
        `success: ${slopeLabel(srSlope, srN)}`,
        `cost: ${slopeLabel(costSlope, costN)}`,
        `rework: ${slopeLabel(rwSlope, rwN)}`,
    ].join(' · ');

    if (sub) sub.textContent = slopeInfo;

    if (trendChartInstance) {
        trendChartInstance.destroy();
        trendChartInstance = null;
    }

    trendChartInstance = new Chart(canvas, {
        type: 'line',
        data: {
            labels,
            datasets: [
                {
                    label: 'Success Rate (%)',
                    data: toValues(srPts),
                    borderColor: '#22c55e',
                    backgroundColor: 'rgba(34,197,94,0.08)',
                    tension: 0.25,
                    spanGaps: false,
                    yAxisID: 'yPct',
                },
                {
                    label: 'Rework Rate (%)',
                    data: toValues(rwPts),
                    borderColor: '#ef4444',
                    backgroundColor: 'rgba(239,68,68,0.08)',
                    tension: 0.25,
                    spanGaps: false,
                    borderDash: [4, 3],
                    yAxisID: 'yPct',
                },
                {
                    label: 'Avg Cost/Run ($)',
                    data: toValues(costPts),
                    borderColor: '#f59e0b',
                    backgroundColor: 'rgba(245,158,11,0.08)',
                    tension: 0.25,
                    spanGaps: false,
                    borderDash: [2, 2],
                    yAxisID: 'yCost',
                },
            ],
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: { mode: 'index', intersect: false },
            plugins: {
                legend: { display: true, labels: { font: { size: 10 } } },
                tooltip: {
                    callbacks: {
                        label(ctx) {
                            const v = ctx.parsed.y;
                            if (v === null) return `${ctx.dataset.label}: —`;
                            if (ctx.dataset.yAxisID === 'yCost') return `${ctx.dataset.label}: $${v.toFixed(3)}`;
                            return `${ctx.dataset.label}: ${v.toFixed(1)}%`;
                        },
                    },
                },
            },
            scales: {
                x: {
                    ticks: { font: { size: 10 }, maxRotation: 45 },
                },
                yPct: {
                    type: 'linear',
                    position: 'left',
                    min: 0,
                    max: 100,
                    title: { display: true, text: '%', font: { size: 10 } },
                    ticks: { font: { size: 10 } },
                },
                yCost: {
                    type: 'linear',
                    position: 'right',
                    title: { display: true, text: '$', font: { size: 10 } },
                    ticks: { font: { size: 10 } },
                    grid: { drawOnChartArea: false },
                },
            },
        },
    });
}
