// Composable: Chart.js lifecycle for the metrics speed graph.
import { ref, nextTick } from 'vue';
import { Chart, LineController, LineElement, PointElement, LinearScale, CategoryScale, Filler, Legend, Tooltip } from 'chart.js';
import { fmtRate, fmtRateShort } from '../utils/metrics-format.js';

Chart.register(LineController, LineElement, PointElement, LinearScale, CategoryScale, Filler, Legend, Tooltip);

const COLORS = {
	dl: { border: '#3498db', bg: 'rgba(52,152,219,0.12)' },
	ul: { border: '#e67e22', bg: 'rgba(230,126,34,0.12)' },
	grid: '#2e3d57',
	text: '#949b9f',
};

/**
 * Creates and manages a Chart.js line chart for download/upload rates.
 *
 * @returns {{
 *   chartCanvas: import('vue').Ref<HTMLCanvasElement|null>,
 *   CHART_H: number,
 *   initCharts: () => void,
 *   destroyCharts: () => void,
 *   updateCharts: (chartData: object|null) => void,
 * }}
 */
export function useMetricsChart() {
	const chartCanvas = ref(null);
	let chart = null;
	const CHART_H = 250;

	function makeChartCfg() {
		return {
			type: 'line',
			data: {
				labels: [],
				datasets: [
					{
						label: '↓ Download',
						data: [],
						borderColor: COLORS.dl.border,
						backgroundColor: COLORS.dl.bg,
						fill: true, tension: 0.3,
						pointRadius: 2, pointHoverRadius: 5, borderWidth: 2,
					},
					{
						label: '↑ Upload',
						data: [],
						borderColor: COLORS.ul.border,
						backgroundColor: COLORS.ul.bg,
						fill: true, tension: 0.3,
						pointRadius: 2, pointHoverRadius: 5, borderWidth: 2,
					},
				],
			},
			options: {
				responsive: true,
				maintainAspectRatio: false,
				animation: { duration: 200 },
				plugins: {
					legend: {
						position: 'top',
						align: 'end',
						labels: {
							color: COLORS.text,
							font: { size: 11 },
							boxWidth: 12, padding: 10,
							usePointStyle: true, pointStyle: 'circle',
						},
					},
					tooltip: {
						mode: 'index', intersect: false,
						backgroundColor: '#2e3d57',
						titleColor: '#c2c2c2', bodyColor: '#c2c2c2',
						borderColor: '#4d545f', borderWidth: 1, padding: 8,
						titleFont: { size: 11 }, bodyFont: { size: 11 },
						callbacks: {
							label(ctx) { return ctx.dataset.label + ': ' + fmtRate(ctx.parsed.y); },
						},
					},
				},
				scales: {
					x: {
						ticks: { color: COLORS.text, font: { size: 10 }, maxRotation: 0, maxTicksLimit: 10 },
						grid: { color: COLORS.grid, lineWidth: 0.5 },
						border: { color: COLORS.grid },
					},
					y: {
						ticks: {
							color: COLORS.text, font: { size: 10 }, maxTicksLimit: 5,
							callback(v) { return fmtRateShort(v); },
						},
						grid: { color: COLORS.grid, lineWidth: 0.5 },
						border: { color: COLORS.grid },
						beginAtZero: true,
					},
				},
				interaction: { mode: 'nearest', axis: 'x', intersect: false },
			},
		};
	}

	function initCharts() {
		destroyCharts();
		if (chartCanvas.value) {
			chart = new Chart(chartCanvas.value.getContext('2d'), makeChartCfg());
		}
	}

	function destroyCharts() {
		if (chart) { chart.destroy(); chart = null; }
	}

	function updateCharts(chartData) {
		if (!chartData || !chartData.labels.length || !chart) return;
		const maxPts = 60;
		const slice = chartData.labels.length > maxPts ? chartData.labels.length - maxPts : 0;
		chart.data.labels = chartData.labels.slice(slice);
		chart.data.datasets[0].data = chartData.dl.slice(slice);
		chart.data.datasets[1].data = chartData.ul.slice(slice);
		chart.update('none');
	}

	return { chartCanvas, CHART_H, initCharts, destroyCharts, updateCharts };
}
