import { writeFileSync } from 'node:fs'

const datasource = {
	type: 'prometheus',
	uid: '${DS_PROMETHEUS}',
}

const processorFilter = '{job=~"$processor_job",instance=~"$processor_instance"}'
const alerterFilter = '{job=~"$alerter_job",instance=~"$alerter_instance"}'

let nextPanelId = 1

function nextRef(index) {
	return String.fromCharCode(65 + index)
}

function withRefs(targets) {
	return targets.map((target, index) => ({
		refId: nextRef(index),
		...target,
	}))
}

function thresholds(...steps) {
	return {
		mode: 'absolute',
		steps,
	}
}

function statPanel({
	title,
	expr,
	x,
	y,
	w = 3,
	h = 4,
	unit = 'none',
	decimals = 2,
	mappings = [],
	thresholdSteps = [{ color: 'green', value: null }],
}) {
	return {
		datasource,
		fieldConfig: {
			defaults: {
				color: {
					mode: 'thresholds',
				},
				decimals,
				mappings,
				thresholds: thresholds(...thresholdSteps),
				unit,
			},
			overrides: [],
		},
		gridPos: { h, w, x, y },
		id: nextPanelId++,
		options: {
			colorMode: 'value',
			graphMode: 'area',
			justifyMode: 'auto',
			orientation: 'auto',
			reduceOptions: {
				calcs: ['lastNotNull'],
				fields: '',
				values: false,
			},
			textMode: 'auto',
		},
		targets: withRefs([{ expr, legendFormat: '' }]),
		title,
		type: 'stat',
	}
}

function timeseriesPanel({
	title,
	targets,
	x,
	y,
	w = 12,
	h = 8,
	unit = 'short',
	decimals = 2,
	stacking = 'none',
}) {
	return {
		datasource,
		fieldConfig: {
			defaults: {
				color: {
					mode: 'palette-classic',
				},
				custom: {
					axisBorderShow: false,
					axisCenteredZero: false,
					axisColorMode: 'text',
					axisLabel: '',
					axisPlacement: 'auto',
					barAlignment: 0,
					drawStyle: 'line',
					fillOpacity: 12,
					gradientMode: 'none',
					hideFrom: {
						legend: false,
						tooltip: false,
						viz: false,
					},
					insertNulls: false,
					lineInterpolation: 'linear',
					lineWidth: 2,
					pointSize: 4,
					scaleDistribution: {
						type: 'linear',
					},
					showPoints: 'auto',
					spanNulls: true,
					stacking: {
						group: 'A',
						mode: stacking,
					},
					thresholdsStyle: {
						mode: 'off',
					},
				},
				decimals,
				thresholds: thresholds({ color: 'green', value: null }),
				unit,
			},
			overrides: [],
		},
		gridPos: { h, w, x, y },
		id: nextPanelId++,
		options: {
			legend: {
				calcs: [],
				displayMode: 'list',
				placement: 'bottom',
				showLegend: true,
			},
			tooltip: {
				mode: 'multi',
				sort: 'desc',
			},
		},
		targets: withRefs(targets),
		title,
		type: 'timeseries',
	}
}

function rowPanel(title, y) {
	return {
		collapsed: false,
		gridPos: { h: 1, w: 24, x: 0, y },
		id: nextPanelId++,
		panels: [],
		title,
		type: 'row',
	}
}

function resetPanelIds() {
	nextPanelId = 1
}

function commonTemplating() {
	return [
		{
			current: {
				selected: true,
				text: 'All',
				value: ['$__all'],
			},
			datasource,
			definition: 'label_values(poracle_processor_worker_pool_capacity, job)',
			hide: 0,
			includeAll: true,
			label: 'Processor job',
			multi: true,
			name: 'processor_job',
			options: [],
			query: 'label_values(poracle_processor_worker_pool_capacity, job)',
			refresh: 1,
			sort: 1,
			type: 'query',
		},
		{
			current: {
				selected: true,
				text: 'All',
				value: ['$__all'],
			},
			datasource,
			definition: 'label_values(poracle_processor_worker_pool_capacity{job=~"$processor_job"}, instance)',
			hide: 0,
			includeAll: true,
			label: 'Processor instance',
			multi: true,
			name: 'processor_instance',
			options: [],
			query: 'label_values(poracle_processor_worker_pool_capacity{job=~"$processor_job"}, instance)',
			refresh: 2,
			sort: 1,
			type: 'query',
		},
		{
			current: {
				selected: true,
				text: 'All',
				value: ['$__all'],
			},
			datasource,
			definition: 'label_values(process_resident_memory_bytes{job!~"$processor_job"}, job)',
			hide: 0,
			includeAll: true,
			label: 'Alerter job',
			multi: true,
			name: 'alerter_job',
			options: [],
			query: 'label_values(process_resident_memory_bytes{job!~"$processor_job"}, job)',
			refresh: 1,
			sort: 1,
			type: 'query',
		},
		{
			current: {
				selected: true,
				text: 'All',
				value: ['$__all'],
			},
			datasource,
			definition: 'label_values(process_resident_memory_bytes{job=~"$alerter_job"}, instance)',
			hide: 0,
			includeAll: true,
			label: 'Alerter instance',
			multi: true,
			name: 'alerter_instance',
			options: [],
			query: 'label_values(process_resident_memory_bytes{job=~"$alerter_job"}, instance)',
			refresh: 2,
			sort: 1,
			type: 'query',
		},
	]
}

function buildDashboard({ title, uid, version, description, panels }) {
	return {
		__inputs: [
			{
				name: 'DS_PROMETHEUS',
				label: 'Prometheus',
				description: '',
				type: 'datasource',
				pluginId: 'prometheus',
				pluginName: 'Prometheus',
			},
		],
		annotations: {
			list: [
				{
					builtIn: 1,
					datasource: {
						type: 'grafana',
						uid: '-- Grafana --',
					},
					enable: true,
					hide: true,
					iconColor: 'rgba(0, 211, 255, 1)',
					name: 'Annotations & Alerts',
					type: 'dashboard',
				},
			],
		},
		description,
		editable: true,
		fiscalYearStartMonth: 0,
		graphTooltip: 0,
		id: null,
		links: [],
		liveNow: false,
		panels,
		refresh: '30s',
		schemaVersion: 39,
		style: 'dark',
		tags: ['poracle', 'prometheus', 'grafana'],
		templating: {
			list: commonTemplating(),
		},
		time: {
			from: 'now-6h',
			to: 'now',
		},
		timepicker: {},
		timezone: 'browser',
		title,
		uid,
		version,
		weekStart: '',
	}
}

// ─── Full Observability Dashboard ────────────────────────────────────────────

const panels = []

let y = 0

panels.push(rowPanel('Overview', y))
y += 1

const upMappings = [
	{
		options: {
			0: { text: 'Down' },
			1: { text: 'Up' },
		},
		type: 'value',
	},
]

panels.push(
	statPanel({
		title: 'Processor Up',
		expr: `min(up{job=~"$processor_job",instance=~"$processor_instance"})`,
		x: 0,
		y,
		mappings: upMappings,
		thresholdSteps: [
			{ color: 'red', value: null },
			{ color: 'green', value: 1 },
		],
	}),
)
panels.push(
	statPanel({
		title: 'Alerter Up',
		expr: `min(up{job=~"$alerter_job",instance=~"$alerter_instance"})`,
		x: 3,
		y,
		mappings: upMappings,
		thresholdSteps: [
			{ color: 'red', value: null },
			{ color: 'green', value: 1 },
		],
	}),
)
panels.push(
	statPanel({
		title: 'Webhooks/s',
		expr: `sum(rate(poracle_processor_webhooks_received_total${processorFilter}[$__rate_interval]))`,
		x: 6,
		y,
		unit: 'ops',
	}),
)
panels.push(
	statPanel({
		title: 'Matched Users/s',
		expr: `sum(rate(poracle_processor_matched_users_total${processorFilter}[$__rate_interval]))`,
		x: 9,
		y,
		unit: 'ops',
	}),
)
panels.push(
	statPanel({
		title: 'Delivered/s',
		expr: `sum(rate(poracle_delivery_total${processorFilter}[$__rate_interval]))`,
		x: 12,
		y,
		unit: 'ops',
	}),
)
panels.push(
	statPanel({
		title: 'Delivery Failure %',
		expr:
			`100 * sum(rate(poracle_delivery_total{status!="ok",job=~"$processor_job",instance=~"$processor_instance"}[$__rate_interval])) / ` +
			`clamp_min(sum(rate(poracle_delivery_total${processorFilter}[$__rate_interval])), 0.001)`,
		x: 15,
		y,
		unit: 'percent',
		thresholdSteps: [
			{ color: 'green', value: null },
			{ color: 'yellow', value: 1 },
			{ color: 'red', value: 5 },
		],
	}),
)
panels.push(
	statPanel({
		title: 'Worker Pool Used %',
		expr:
			`100 * sum(poracle_processor_worker_pool_in_use${processorFilter}) / ` +
			`clamp_min(sum(poracle_processor_worker_pool_capacity${processorFilter}), 1)`,
		x: 18,
		y,
		unit: 'percent',
		thresholdSteps: [
			{ color: 'green', value: null },
			{ color: 'yellow', value: 70 },
			{ color: 'red', value: 90 },
		],
	}),
)
panels.push(
	statPanel({
		title: 'Render Queue %',
		expr:
			`100 * sum(poracle_render_queue_depth${processorFilter}) / ` +
			`clamp_min(sum(poracle_render_queue_capacity${processorFilter}), 1)`,
		x: 21,
		y,
		unit: 'percent',
		thresholdSteps: [
			{ color: 'green', value: null },
			{ color: 'yellow', value: 70 },
			{ color: 'red', value: 90 },
		],
	}),
)
y += 4

// ─── Processor Pipeline ──────────────────────────────────────────────────────

panels.push(rowPanel('Processor Pipeline', y))
y += 1

panels.push(
	timeseriesPanel({
		title: 'Webhook Intake by Type',
		x: 0,
		y,
		unit: 'ops',
		targets: [
			{
				expr: `sum by(type) (rate(poracle_processor_webhooks_received_total${processorFilter}[$__rate_interval]))`,
				legendFormat: '{{type}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Webhook Processing p95 by Type',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, type) (rate(poracle_processor_webhook_processing_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: '{{type}}',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Match Production by Type',
		x: 0,
		y,
		unit: 'ops',
		targets: [
			{
				expr: `sum by(type) (rate(poracle_processor_matched_events_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'matched events {{type}}',
			},
			{
				expr: `sum by(type) (rate(poracle_processor_matched_users_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'matched users {{type}}',
			},
			{
				expr: `sum by(type) (rate(poracle_processor_duplicates_skipped_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'duplicates {{type}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Worker Pool Pressure',
		x: 12,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum(poracle_processor_worker_pool_in_use${processorFilter})`,
				legendFormat: 'worker pool in use',
			},
			{
				expr: `sum(poracle_processor_worker_pool_capacity${processorFilter})`,
				legendFormat: 'worker pool capacity',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Matching Duration p95 by Type',
		x: 0,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, type) (rate(poracle_processor_matching_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: '{{type}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Enrichment Duration p95 by Type',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, type) (rate(poracle_processor_enrichment_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: '{{type}}',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Reloads and Rate Limiter',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum by(status) (rate(poracle_processor_state_reloads_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'reloads {{status}}',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_processor_state_reload_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'reload p95',
			},
			{
				expr: `sum(rate(poracle_processor_rate_limit_dropped_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'rate limit dropped',
			},
			{
				expr: `sum(rate(poracle_processor_rate_limit_breaches_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'rate limit breaches',
			},
			{
				expr: `sum(rate(poracle_processor_rate_limit_disabled_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'rate limit disabled',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Match Yield per Webhook',
		x: 12,
		y,
		unit: 'short',
		targets: [
			{
				expr:
					`sum(rate(poracle_processor_matched_users_total${processorFilter}[$__rate_interval])) / ` +
					`clamp_min(sum(rate(poracle_processor_webhooks_received_total${processorFilter}[$__rate_interval])), 0.001)`,
				legendFormat: 'matched users per webhook',
			},
			{
				expr:
					`sum(rate(poracle_processor_matched_events_total${processorFilter}[$__rate_interval])) / ` +
					`clamp_min(sum(rate(poracle_processor_webhooks_received_total${processorFilter}[$__rate_interval])), 0.001)`,
				legendFormat: 'matched events per webhook',
			},
		],
	}),
)
y += 8

// ─── Render Pipeline ─────────────────────────────────────────────────────────

panels.push(rowPanel('Render Pipeline', y))
y += 1

panels.push(
	timeseriesPanel({
		title: 'Render Queue Depth and Capacity',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum(poracle_render_queue_depth${processorFilter})`,
				legendFormat: 'queue depth',
			},
			{
				expr: `sum(poracle_render_queue_capacity${processorFilter})`,
				legendFormat: 'queue capacity',
			},
			{
				expr: `sum(rate(poracle_render_tile_skipped_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'tiles skipped (backpressure)',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Render Job Duration p95',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_render_duration_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'render job p95',
			},
			{
				expr:
					`sum(rate(poracle_render_duration_seconds_sum${processorFilter}[$__rate_interval])) / ` +
					`clamp_min(sum(rate(poracle_render_duration_seconds_count${processorFilter}[$__rate_interval])), 0.001)`,
				legendFormat: 'render job avg',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Render Outcomes',
		x: 0,
		y,
		unit: 'ops',
		targets: [
			{
				expr: `sum by(status) (rate(poracle_render_total${processorFilter}[$__rate_interval]))`,
				legendFormat: '{{status}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Template Render Duration p95 by Type',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, type) (rate(poracle_template_render_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: '{{type}}',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Template Render Outcomes by Type',
		x: 0,
		y,
		unit: 'ops',
		targets: [
			{
				expr: `sum by(type, status) (rate(poracle_template_render_total${processorFilter}[$__rate_interval]))`,
				legendFormat: '{{type}} {{status}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Tile Mode Decisions',
		x: 12,
		y,
		unit: 'ops',
		targets: [
			{
				expr: `sum by(mode) (rate(poracle_tile_mode_total${processorFilter}[$__rate_interval]))`,
				legendFormat: '{{mode}}',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Shlink URL Shortening',
		x: 12,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum by(result) (rate(poracle_shlink_total${processorFilter}[$__rate_interval]))`,
				legendFormat: '{{result}}',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_shlink_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'latency p95',
			},
		],
	}),
)
y += 8

// ─── Delivery ────────────────────────────────────────────────────────────────

panels.push(rowPanel('Delivery', y))
y += 1

panels.push(
	timeseriesPanel({
		title: 'Delivery Queue Depths',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum(poracle_delivery_queue_depth${processorFilter})`,
				legendFormat: 'total queue',
			},
			{
				expr: `sum(poracle_delivery_discord_queue_depth${processorFilter})`,
				legendFormat: 'discord',
			},
			{
				expr: `sum(poracle_delivery_webhook_queue_depth${processorFilter})`,
				legendFormat: 'discord webhook',
			},
			{
				expr: `sum(poracle_delivery_telegram_queue_depth${processorFilter})`,
				legendFormat: 'telegram',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Delivery Outcomes by Platform',
		x: 12,
		y,
		unit: 'ops',
		targets: [
			{
				expr: `sum by(platform, status) (rate(poracle_delivery_total${processorFilter}[$__rate_interval]))`,
				legendFormat: '{{platform}} {{status}}',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Delivery Latency p95 by Platform',
		x: 0,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, platform) (rate(poracle_delivery_duration_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: '{{platform}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Delivery In-Flight and Tracker',
		x: 12,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum by(platform) (poracle_delivery_in_flight${processorFilter})`,
				legendFormat: 'in-flight {{platform}}',
			},
			{
				expr: `sum(poracle_delivery_tracker_size${processorFilter})`,
				legendFormat: 'tracker size',
			},
			{
				expr: `sum(rate(poracle_delivery_clean_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'clean deletes/s',
			},
			{
				expr: `sum(rate(poracle_delivery_tracker_evictions_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'tracker evictions/s',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Delivery Rate Limits',
		x: 0,
		y,
		unit: 'ops',
		targets: [
			{
				expr: `sum by(platform) (rate(poracle_delivery_rate_limited_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'rate limited {{platform}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Discord Rate Limit Wait Time',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, platform) (rate(poracle_delivery_rate_limit_wait_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: '{{platform}} p95',
			},
			{
				expr:
					`sum by(platform) (rate(poracle_delivery_rate_limit_wait_seconds_sum${processorFilter}[$__rate_interval])) / ` +
					`clamp_min(sum by(platform) (rate(poracle_delivery_rate_limit_wait_seconds_count${processorFilter}[$__rate_interval])), 0.001)`,
				legendFormat: '{{platform}} avg',
			},
		],
	}),
)
y += 8

// ─── Processor Integrations ──────────────────────────────────────────────────

panels.push(rowPanel('Processor Integrations', y))
y += 1

panels.push(
	timeseriesPanel({
		title: 'Geocode Outcomes and In Flight',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum by(result) (rate(poracle_processor_geocode_total${processorFilter}[$__rate_interval]))`,
				legendFormat: '{{result}}',
			},
			{
				expr: `sum(poracle_processor_geocode_in_flight${processorFilter})`,
				legendFormat: 'in flight',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Geocode Latency',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`sum(rate(poracle_processor_geocode_seconds_sum${processorFilter}[$__rate_interval])) / ` +
					`clamp_min(sum(rate(poracle_processor_geocode_seconds_count${processorFilter}[$__rate_interval])), 0.001)`,
				legendFormat: 'avg',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_processor_geocode_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'p95',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Tile Outcomes and In Flight',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum by(result) (rate(poracle_processor_tile_total${processorFilter}[$__rate_interval]))`,
				legendFormat: '{{result}}',
			},
			{
				expr: `sum(poracle_processor_tile_in_flight${processorFilter})`,
				legendFormat: 'in flight',
			},
			{
				expr: `sum(poracle_processor_tile_queue_depth${processorFilter})`,
				legendFormat: 'async queue depth',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Tile Latency',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`sum(rate(poracle_processor_tile_seconds_sum${processorFilter}[$__rate_interval])) / ` +
					`clamp_min(sum(rate(poracle_processor_tile_seconds_count${processorFilter}[$__rate_interval])), 0.001)`,
				legendFormat: 'avg',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_processor_tile_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'p95',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Circuit Breakers',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum(poracle_tile_circuit_healthy${processorFilter})`,
				legendFormat: 'tileserver circuit (1=healthy)',
			},
			{
				expr: `sum(poracle_geocode_circuit_healthy${processorFilter})`,
				legendFormat: 'geocode circuit (1=healthy)',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Uicons Index Refresh',
		x: 12,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum by(result) (rate(poracle_uicons_refresh_total${processorFilter}[$__rate_interval]))`,
				legendFormat: '{{result}}',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_uicons_refresh_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'latency p95',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'AccuWeather Requests by Type and Status',
		x: 0,
		y,
		unit: 'ops',
		targets: [
			{
				expr:
					`sum by(type, status) (` +
					`rate(poracle_processor_accuweather_requests_total${processorFilter}[$__rate_interval]))`,
				legendFormat: '{{type}} {{status}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'AccuWeather Quota and Key Usage',
		x: 12,
		y,
		unit: 'short',
		targets: [
			{
				expr: `max by(key_index) (poracle_processor_accuweather_quota_remaining${processorFilter})`,
				legendFormat: 'quota remaining key {{key_index}}',
			},
			{
				expr: `max by(key_index) (poracle_processor_accuweather_key_usage_today${processorFilter})`,
				legendFormat: 'usage today key {{key_index}}',
			},
			{
				expr: `sum(rate(poracle_processor_accuweather_quota_exhausted_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'quota exhausted/s',
			},
		],
	}),
)
y += 8

// ─── State & API ─────────────────────────────────────────────────────────────

panels.push(rowPanel('State & API', y))
y += 1

panels.push(
	timeseriesPanel({
		title: 'State Size',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum(poracle_processor_state_humans${processorFilter})`,
				legendFormat: 'humans',
			},
			{
				expr: `sum by(type) (poracle_processor_state_tracking_rules${processorFilter})`,
				legendFormat: 'rules {{type}}',
			},
			{
				expr: `sum(poracle_processor_state_geofences${processorFilter})`,
				legendFormat: 'geofences',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'State Reload Breakdown',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_processor_state_reload_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'total reload p95',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_processor_state_db_query_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'DB query p95',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'API Request Rate',
		x: 0,
		y,
		unit: 'ops',
		targets: [
			{
				expr: `sum by(method, endpoint) (rate(poracle_processor_api_requests_total${processorFilter}[$__rate_interval]))`,
				legendFormat: '{{method}} {{endpoint}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'API Request Latency p95',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, endpoint) (rate(poracle_processor_api_request_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: '{{endpoint}}',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Webhook Batch Size',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_processor_webhook_batch_size_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'batch size p95',
			},
			{
				expr:
					`sum(rate(poracle_processor_webhook_batch_size_sum${processorFilter}[$__rate_interval])) / ` +
					`clamp_min(sum(rate(poracle_processor_webhook_batch_size_count${processorFilter}[$__rate_interval])), 0.001)`,
				legendFormat: 'batch size avg',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Last Successful Reload',
		x: 12,
		y,
		unit: 'dateTimeAsIso',
		targets: [
			{
				expr: `poracle_processor_state_last_reload_success_timestamp${processorFilter} * 1000`,
				legendFormat: '{{instance}}',
			},
		],
	}),
)
y += 8

panels.push(
	statPanel({
		title: 'Build Info',
		expr: `poracle_processor_build_info${processorFilter}`,
		x: 0,
		y,
		w: 24,
		h: 3,
		thresholdSteps: [{ color: 'blue', value: null }],
	}),
)
y += 3

// ─── Runtime and Process Health ──────────────────────────────────────────────

panels.push(rowPanel('Runtime and Process Health', y))
y += 1

panels.push(
	timeseriesPanel({
		title: 'Process Memory',
		x: 0,
		y,
		unit: 'bytes',
		targets: [
			{
				expr: `process_resident_memory_bytes{job=~"$processor_job",instance=~"$processor_instance"}`,
				legendFormat: 'processor rss {{instance}}',
			},
			{
				expr: `process_virtual_memory_bytes{job=~"$processor_job",instance=~"$processor_instance"}`,
				legendFormat: 'processor virtual {{instance}}',
			},
			{
				expr: `process_resident_memory_bytes{job=~"$alerter_job",instance=~"$alerter_instance"}`,
				legendFormat: 'alerter rss {{instance}}',
			},
			{
				expr: `process_virtual_memory_bytes{job=~"$alerter_job",instance=~"$alerter_instance"}`,
				legendFormat: 'alerter virtual {{instance}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Process CPU',
		x: 12,
		y,
		unit: 'cores',
		targets: [
			{
				expr:
					`rate(process_cpu_seconds_total{job=~"$processor_job",instance=~"$processor_instance"}[$__rate_interval])`,
				legendFormat: 'processor {{instance}}',
			},
			{
				expr:
					`rate(process_cpu_seconds_total{job=~"$alerter_job",instance=~"$alerter_instance"}[$__rate_interval])`,
				legendFormat: 'alerter {{instance}}',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Go Scheduler',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `go_goroutines{job=~"$processor_job",instance=~"$processor_instance"}`,
				legendFormat: 'goroutines {{instance}}',
			},
			{
				expr: `go_threads{job=~"$processor_job",instance=~"$processor_instance"}`,
				legendFormat: 'threads {{instance}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Go Heap',
		x: 12,
		y,
		unit: 'bytes',
		targets: [
			{
				expr: `go_memstats_heap_alloc_bytes{job=~"$processor_job",instance=~"$processor_instance"}`,
				legendFormat: 'heap alloc {{instance}}',
			},
			{
				expr: `go_memstats_heap_inuse_bytes{job=~"$processor_job",instance=~"$processor_instance"}`,
				legendFormat: 'heap in use {{instance}}',
			},
			{
				expr: `go_memstats_stack_inuse_bytes{job=~"$processor_job",instance=~"$processor_instance"}`,
				legendFormat: 'stack in use {{instance}}',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Go GC Duration',
		x: 0,
		y,
		unit: 's',
		targets: [
			{
				expr: `go_gc_duration_seconds{job=~"$processor_job",instance=~"$processor_instance",quantile="0.99"}`,
				legendFormat: 'gc p99 {{instance}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Node.js Heap',
		x: 12,
		y,
		unit: 'bytes',
		targets: [
			{
				expr: `nodejs_heap_size_used_bytes{job=~"$alerter_job",instance=~"$alerter_instance"}`,
				legendFormat: 'heap used {{instance}}',
			},
			{
				expr: `nodejs_heap_size_total_bytes{job=~"$alerter_job",instance=~"$alerter_instance"}`,
				legendFormat: 'heap total {{instance}}',
			},
			{
				expr: `nodejs_external_memory_bytes{job=~"$alerter_job",instance=~"$alerter_instance"}`,
				legendFormat: 'external {{instance}}',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Node.js Event Loop and GC',
		x: 0,
		y,
		unit: 's',
		targets: [
			{
				expr: `nodejs_eventloop_lag_seconds{job=~"$alerter_job",instance=~"$alerter_instance"}`,
				legendFormat: 'event loop lag {{instance}}',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(nodejs_gc_duration_seconds_bucket{job=~"$alerter_job",instance=~"$alerter_instance"}[$__rate_interval])))`,
				legendFormat: 'gc p95',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Node.js Handles and Requests',
		x: 12,
		y,
		unit: 'short',
		targets: [
			{
				expr: `nodejs_active_handles_total{job=~"$alerter_job",instance=~"$alerter_instance"}`,
				legendFormat: 'handles {{instance}}',
			},
			{
				expr: `nodejs_active_requests_total{job=~"$alerter_job",instance=~"$alerter_instance"}`,
				legendFormat: 'requests {{instance}}',
			},
		],
	}),
)

const dashboard = buildDashboard({
	title: 'PoracleNG Observability',
	uid: 'poracleng-observability',
	version: 3,
	description:
		'Complete observability dashboard for PoracleNG processor and alerter Prometheus metrics, including render pipeline, delivery, and runtime telemetry.',
	panels,
})

// ─── Operations Lite Dashboard ───────────────────────────────────────────────

resetPanelIds()

const litePanels = []
let liteY = 0

litePanels.push(rowPanel('Ops Overview', liteY))
liteY += 1

litePanels.push(
	statPanel({
		title: 'Processor Up',
		expr: `min(up{job=~"$processor_job",instance=~"$processor_instance"})`,
		x: 0,
		y: liteY,
		mappings: upMappings,
		thresholdSteps: [
			{ color: 'red', value: null },
			{ color: 'green', value: 1 },
		],
	}),
)
litePanels.push(
	statPanel({
		title: 'Alerter Up',
		expr: `min(up{job=~"$alerter_job",instance=~"$alerter_instance"})`,
		x: 3,
		y: liteY,
		mappings: upMappings,
		thresholdSteps: [
			{ color: 'red', value: null },
			{ color: 'green', value: 1 },
		],
	}),
)
litePanels.push(
	statPanel({
		title: 'Webhooks/s',
		expr: `sum(rate(poracle_processor_webhooks_received_total${processorFilter}[$__rate_interval]))`,
		x: 6,
		y: liteY,
		unit: 'ops',
	}),
)
litePanels.push(
	statPanel({
		title: 'Matched Users/s',
		expr: `sum(rate(poracle_processor_matched_users_total${processorFilter}[$__rate_interval]))`,
		x: 9,
		y: liteY,
		unit: 'ops',
	}),
)
litePanels.push(
	statPanel({
		title: 'Delivered/s',
		expr: `sum(rate(poracle_delivery_total${processorFilter}[$__rate_interval]))`,
		x: 12,
		y: liteY,
		unit: 'ops',
	}),
)
litePanels.push(
	statPanel({
		title: 'Delivery Failure %',
		expr:
			`100 * sum(rate(poracle_delivery_total{status!="ok",job=~"$processor_job",instance=~"$processor_instance"}[$__rate_interval])) / ` +
			`clamp_min(sum(rate(poracle_delivery_total${processorFilter}[$__rate_interval])), 0.001)`,
		x: 15,
		y: liteY,
		unit: 'percent',
		thresholdSteps: [
			{ color: 'green', value: null },
			{ color: 'yellow', value: 1 },
			{ color: 'red', value: 5 },
		],
	}),
)
litePanels.push(
	statPanel({
		title: 'Worker Pool Used %',
		expr:
			`100 * sum(poracle_processor_worker_pool_in_use${processorFilter}) / ` +
			`clamp_min(sum(poracle_processor_worker_pool_capacity${processorFilter}), 1)`,
		x: 18,
		y: liteY,
		unit: 'percent',
		thresholdSteps: [
			{ color: 'green', value: null },
			{ color: 'yellow', value: 70 },
			{ color: 'red', value: 90 },
		],
	}),
)
litePanels.push(
	statPanel({
		title: 'Render Queue %',
		expr:
			`100 * sum(poracle_render_queue_depth${processorFilter}) / ` +
			`clamp_min(sum(poracle_render_queue_capacity${processorFilter}), 1)`,
		x: 21,
		y: liteY,
		unit: 'percent',
		thresholdSteps: [
			{ color: 'green', value: null },
			{ color: 'yellow', value: 70 },
			{ color: 'red', value: 90 },
		],
	}),
)
liteY += 4

litePanels.push(rowPanel('Flow Health', liteY))
liteY += 1

litePanels.push(
	timeseriesPanel({
		title: 'Intake and Match Rate',
		x: 0,
		y: liteY,
		unit: 'ops',
		targets: [
			{
				expr: `sum by(type) (rate(poracle_processor_webhooks_received_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'webhooks {{type}}',
			},
			{
				expr: `sum by(type) (rate(poracle_processor_matched_users_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'matched users {{type}}',
			},
		],
	}),
)
litePanels.push(
	timeseriesPanel({
		title: 'Processing Latency and Pressure',
		x: 12,
		y: liteY,
		unit: 'short',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, type) (rate(poracle_processor_webhook_processing_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'p95 {{type}}',
			},
			{
				expr: `sum(poracle_render_queue_depth${processorFilter})`,
				legendFormat: 'render queue',
			},
			{
				expr: `sum(poracle_processor_worker_pool_in_use${processorFilter})`,
				legendFormat: 'workers in use',
			},
		],
	}),
)
liteY += 8

litePanels.push(
	timeseriesPanel({
		title: 'Delivery Queues',
		x: 0,
		y: liteY,
		unit: 'short',
		targets: [
			{
				expr: `sum(poracle_delivery_queue_depth${processorFilter})`,
				legendFormat: 'total',
			},
			{
				expr: `sum(poracle_delivery_discord_queue_depth${processorFilter})`,
				legendFormat: 'discord',
			},
			{
				expr: `sum(poracle_delivery_webhook_queue_depth${processorFilter})`,
				legendFormat: 'discord webhook',
			},
			{
				expr: `sum(poracle_delivery_telegram_queue_depth${processorFilter})`,
				legendFormat: 'telegram',
			},
		],
	}),
)
litePanels.push(
	timeseriesPanel({
		title: 'Delivery Outcomes and Latency',
		x: 12,
		y: liteY,
		unit: 'short',
		targets: [
			{
				expr: `sum by(platform) (rate(poracle_delivery_total{status="ok",job=~"$processor_job",instance=~"$processor_instance"}[$__rate_interval]))`,
				legendFormat: 'sent {{platform}}',
			},
			{
				expr: `sum by(platform) (rate(poracle_delivery_total{status!="ok",job=~"$processor_job",instance=~"$processor_instance"}[$__rate_interval]))`,
				legendFormat: 'failed {{platform}}',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, platform) (` +
					`rate(poracle_delivery_duration_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: '{{platform}} p95',
			},
		],
	}),
)
liteY += 8

litePanels.push(
	timeseriesPanel({
		title: 'Rate Limits and Drops',
		x: 0,
		y: liteY,
		unit: 'ops',
		targets: [
			{
				expr: `sum(rate(poracle_processor_rate_limit_dropped_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'alert rate limit dropped',
			},
			{
				expr: `sum(rate(poracle_processor_rate_limit_breaches_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'alert rate limit breaches',
			},
			{
				expr: `sum by(platform) (rate(poracle_delivery_rate_limited_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'delivery rate limited {{platform}}',
			},
		],
	}),
)
litePanels.push(
	timeseriesPanel({
		title: 'Maps and Geocoding',
		x: 12,
		y: liteY,
		unit: 'short',
		targets: [
			{
				expr: `sum by(result) (rate(poracle_processor_tile_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'tile {{result}}',
			},
			{
				expr: `sum by(result) (rate(poracle_processor_geocode_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'geocode {{result}}',
			},
		],
	}),
)
liteY += 8

litePanels.push(rowPanel('Runtime', liteY))
liteY += 1

litePanels.push(
	timeseriesPanel({
		title: 'Process Memory',
		x: 0,
		y: liteY,
		unit: 'bytes',
		targets: [
			{
				expr: `process_resident_memory_bytes{job=~"$processor_job",instance=~"$processor_instance"}`,
				legendFormat: 'processor rss {{instance}}',
			},
			{
				expr: `process_resident_memory_bytes{job=~"$alerter_job",instance=~"$alerter_instance"}`,
				legendFormat: 'alerter rss {{instance}}',
			},
		],
	}),
)
litePanels.push(
	timeseriesPanel({
		title: 'Process CPU',
		x: 12,
		y: liteY,
		unit: 'cores',
		targets: [
			{
				expr:
					`rate(process_cpu_seconds_total{job=~"$processor_job",instance=~"$processor_instance"}[$__rate_interval])`,
				legendFormat: 'processor {{instance}}',
			},
			{
				expr:
					`rate(process_cpu_seconds_total{job=~"$alerter_job",instance=~"$alerter_instance"}[$__rate_interval])`,
				legendFormat: 'alerter {{instance}}',
			},
		],
	}),
)

const liteDashboard = buildDashboard({
	title: 'PoracleNG Operations Lite',
	uid: 'poracleng-ops-lite',
	version: 2,
	description:
		'Concise Grafana dashboard for day-to-day PoracleNG operations, focusing on service health, flow pressure, delivery, and runtime.',
	panels: litePanels,
})

writeFileSync(new URL('./poracle-observability-dashboard.json', import.meta.url), `${JSON.stringify(dashboard, null, 2)}\n`)
writeFileSync(new URL('./poracle-operations-lite-dashboard.json', import.meta.url), `${JSON.stringify(liteDashboard, null, 2)}\n`)
