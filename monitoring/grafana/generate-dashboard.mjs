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
			definition: 'label_values(poracle_alerter_matched_queue_depth, job)',
			hide: 0,
			includeAll: true,
			label: 'Alerter job',
			multi: true,
			name: 'alerter_job',
			options: [],
			query: 'label_values(poracle_alerter_matched_queue_depth, job)',
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
			definition: 'label_values(poracle_alerter_matched_queue_depth{job=~"$alerter_job"}, instance)',
			hide: 0,
			includeAll: true,
			label: 'Alerter instance',
			multi: true,
			name: 'alerter_instance',
			options: [],
			query: 'label_values(poracle_alerter_matched_queue_depth{job=~"$alerter_job"}, instance)',
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
		title: 'Messages Sent/s',
		expr: `sum(rate(poracle_alerter_messages_sent_total${alerterFilter}[$__rate_interval]))`,
		x: 12,
		y,
		unit: 'ops',
	}),
)
panels.push(
	statPanel({
		title: 'Delivery Failure %',
		expr:
			`100 * sum(rate(poracle_alerter_messages_failed_total${alerterFilter}[$__rate_interval])) / ` +
			`clamp_min(sum(rate(poracle_alerter_messages_sent_total${alerterFilter}[$__rate_interval])) + ` +
			`sum(rate(poracle_alerter_messages_failed_total${alerterFilter}[$__rate_interval])), 0.001)`,
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
		title: 'Backpressure/s',
		expr: `sum(rate(poracle_alerter_backpressure_events_total${alerterFilter}[$__rate_interval]))`,
		x: 21,
		y,
		unit: 'ops',
	}),
)
y += 4

panels.push(rowPanel('Processor Pipeline', y))
y += 1

panels.push(
	timeseriesPanel({
		title: 'Processor Webhook Intake by Type',
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
		title: 'Processor Webhook Processing p95 by Type',
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
		title: 'Processor Match Production by Type',
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
		title: 'Processor Worker and Sender Pressure',
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
			{
				expr: `sum(poracle_processor_sender_queue_depth${processorFilter})`,
				legendFormat: 'sender queue depth',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Processor Sender Outcomes and Batch Size',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum by(status) (rate(poracle_processor_sender_batches_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'batches {{status}}',
			},
			{
				expr:
					`sum(rate(poracle_processor_sender_batch_size_sum${processorFilter}[$__rate_interval])) / ` +
					`clamp_min(sum(rate(poracle_processor_sender_batch_size_count${processorFilter}[$__rate_interval])), 0.001)`,
				legendFormat: 'avg batch size',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_processor_sender_batch_size_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'batch size p95',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Processor Sender Flush Duration',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`sum(rate(poracle_processor_sender_flush_seconds_sum${processorFilter}[$__rate_interval])) / ` +
					`clamp_min(sum(rate(poracle_processor_sender_flush_seconds_count${processorFilter}[$__rate_interval])), 0.001)`,
				legendFormat: 'flush avg',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_processor_sender_flush_seconds_bucket${processorFilter}[$__rate_interval])))`,
				legendFormat: 'flush p95',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Processor Reloads and Rate Limiter',
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
		title: 'Processor Match Yield per Webhook',
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

panels.push(rowPanel('Processor Integrations', y))
y += 1

panels.push(
	timeseriesPanel({
		title: 'Processor Geocode Outcomes and In Flight',
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
		title: 'Processor Geocode Latency',
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
		title: 'Processor Tile Outcomes and In Flight',
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
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Processor Tile Latency',
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

panels.push(rowPanel('Alerter Delivery', y))
y += 1

panels.push(
	timeseriesPanel({
		title: 'Alerter Queue Depths',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum(poracle_alerter_matched_queue_depth${alerterFilter})`,
				legendFormat: 'matched queue',
			},
			{
				expr: `sum(poracle_alerter_hook_queue_depth${alerterFilter})`,
				legendFormat: 'hook queue',
			},
			{
				expr: `sum(poracle_alerter_discord_queue_depth${alerterFilter})`,
				legendFormat: 'discord queue',
			},
			{
				expr: `sum(poracle_alerter_discord_webhook_queue_depth${alerterFilter})`,
				legendFormat: 'discord webhook queue',
			},
			{
				expr: `sum(poracle_alerter_telegram_queue_depth${alerterFilter})`,
				legendFormat: 'telegram queue',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Alerter Message Creation by Controller and Destination',
		x: 12,
		y,
		unit: 'ops',
		targets: [
			{
				expr:
					`sum by(controller_type, destination_type) (` +
					`rate(poracle_alerter_messages_created_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: '{{controller_type}} -> {{destination_type}}',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Alerter Message Create p95 by Controller',
		x: 0,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, controller_type) (` +
					`rate(poracle_alerter_message_create_seconds_bucket${alerterFilter}[$__rate_interval])))`,
				legendFormat: '{{controller_type}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Alerter Delivery Outcomes by Destination',
		x: 12,
		y,
		unit: 'ops',
		targets: [
			{
				expr:
					`sum by(destination_type) (` +
					`rate(poracle_alerter_messages_sent_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'sent {{destination_type}}',
			},
			{
				expr:
					`sum by(destination_type) (` +
					`rate(poracle_alerter_messages_failed_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'failed {{destination_type}}',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Alerter Delivery Latency p95',
		x: 0,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, destination_type) (` +
					`rate(poracle_alerter_discord_delivery_seconds_bucket${alerterFilter}[$__rate_interval])))`,
				legendFormat: 'discord {{destination_type}}',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (` +
					`rate(poracle_alerter_discord_webhook_delivery_seconds_bucket${alerterFilter}[$__rate_interval])))`,
				legendFormat: 'discord webhook',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le, destination_type) (` +
					`rate(poracle_alerter_telegram_delivery_seconds_bucket${alerterFilter}[$__rate_interval])))`,
				legendFormat: 'telegram {{destination_type}}',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Alerter Rate Limits, Drops and Backpressure',
		x: 12,
		y,
		unit: 'ops',
		targets: [
			{
				expr:
					`sum by(source) (` +
					`rate(poracle_alerter_discord_rate_limits_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'discord {{source}}',
			},
			{
				expr: `sum(rate(poracle_alerter_telegram_rate_limits_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'telegram',
			},
			{
				expr: `sum(rate(poracle_alerter_rate_limited_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'dropped',
			},
			{
				expr: `sum(rate(poracle_alerter_backpressure_events_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'backpressure',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Alerter Geocode Outcomes and In Flight',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum by(result) (rate(poracle_alerter_geocode_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: '{{result}}',
			},
			{
				expr: `sum(poracle_alerter_geocode_in_flight${alerterFilter})`,
				legendFormat: 'in flight',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Alerter Geocode Latency',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`sum(rate(poracle_alerter_geocode_seconds_sum${alerterFilter}[$__rate_interval])) / ` +
					`clamp_min(sum(rate(poracle_alerter_geocode_seconds_count${alerterFilter}[$__rate_interval])), 0.001)`,
				legendFormat: 'avg',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_alerter_geocode_seconds_bucket${alerterFilter}[$__rate_interval])))`,
				legendFormat: 'p95',
			},
		],
	}),
)
y += 8

panels.push(
	timeseriesPanel({
		title: 'Alerter Tile Outcomes and In Flight',
		x: 0,
		y,
		unit: 'short',
		targets: [
			{
				expr: `sum by(result) (rate(poracle_alerter_tile_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: '{{result}}',
			},
			{
				expr: `sum(poracle_alerter_tile_in_flight${alerterFilter})`,
				legendFormat: 'in flight',
			},
		],
	}),
)
panels.push(
	timeseriesPanel({
		title: 'Alerter Tile Latency',
		x: 12,
		y,
		unit: 's',
		targets: [
			{
				expr:
					`sum(rate(poracle_alerter_tile_seconds_sum${alerterFilter}[$__rate_interval])) / ` +
					`clamp_min(sum(rate(poracle_alerter_tile_seconds_count${alerterFilter}[$__rate_interval])), 0.001)`,
				legendFormat: 'avg',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (rate(poracle_alerter_tile_seconds_bucket${alerterFilter}[$__rate_interval])))`,
				legendFormat: 'p95',
			},
		],
	}),
)
y += 8

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
	version: 2,
	description:
		'Complete observability dashboard for PoracleNG processor and alerter Prometheus metrics, including runtime telemetry.',
	panels,
})

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
		title: 'Messages Sent/s',
		expr: `sum(rate(poracle_alerter_messages_sent_total${alerterFilter}[$__rate_interval]))`,
		x: 12,
		y: liteY,
		unit: 'ops',
	}),
)
litePanels.push(
	statPanel({
		title: 'Delivery Failure %',
		expr:
			`100 * sum(rate(poracle_alerter_messages_failed_total${alerterFilter}[$__rate_interval])) / ` +
			`clamp_min(sum(rate(poracle_alerter_messages_sent_total${alerterFilter}[$__rate_interval])) + ` +
			`sum(rate(poracle_alerter_messages_failed_total${alerterFilter}[$__rate_interval])), 0.001)`,
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
		title: 'Backpressure/s',
		expr: `sum(rate(poracle_alerter_backpressure_events_total${alerterFilter}[$__rate_interval]))`,
		x: 21,
		y: liteY,
		unit: 'ops',
	}),
)
liteY += 4

litePanels.push(rowPanel('Flow Health', liteY))
liteY += 1

litePanels.push(
	timeseriesPanel({
		title: 'Processor Intake and Match Rate',
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
		title: 'Processor Latency and Pressure',
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
				expr: `sum(poracle_processor_sender_queue_depth${processorFilter})`,
				legendFormat: 'sender queue',
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
		title: 'Alerter Queues',
		x: 0,
		y: liteY,
		unit: 'short',
		targets: [
			{
				expr: `sum(poracle_alerter_matched_queue_depth${alerterFilter})`,
				legendFormat: 'matched',
			},
			{
				expr: `sum(poracle_alerter_hook_queue_depth${alerterFilter})`,
				legendFormat: 'hook',
			},
			{
				expr: `sum(poracle_alerter_discord_queue_depth${alerterFilter})`,
				legendFormat: 'discord',
			},
			{
				expr: `sum(poracle_alerter_discord_webhook_queue_depth${alerterFilter})`,
				legendFormat: 'discord webhook',
			},
			{
				expr: `sum(poracle_alerter_telegram_queue_depth${alerterFilter})`,
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
				expr:
					`sum by(destination_type) (` +
					`rate(poracle_alerter_messages_sent_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'sent {{destination_type}}',
			},
			{
				expr:
					`sum by(destination_type) (` +
					`rate(poracle_alerter_messages_failed_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'failed {{destination_type}}',
			},
			{
				expr:
					`histogram_quantile(0.95, ` +
					`sum by(le) (` +
					`rate(poracle_alerter_discord_webhook_delivery_seconds_bucket${alerterFilter}[$__rate_interval])))`,
				legendFormat: 'discord webhook p95',
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
				legendFormat: 'processor dropped',
			},
			{
				expr: `sum(rate(poracle_processor_rate_limit_breaches_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'processor breaches',
			},
			{
				expr:
					`sum by(source) (` +
					`rate(poracle_alerter_discord_rate_limits_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'discord {{source}}',
			},
			{
				expr: `sum(rate(poracle_alerter_telegram_rate_limits_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'telegram',
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
				legendFormat: 'processor tile {{result}}',
			},
			{
				expr: `sum by(result) (rate(poracle_processor_geocode_total${processorFilter}[$__rate_interval]))`,
				legendFormat: 'processor geocode {{result}}',
			},
			{
				expr: `sum by(result) (rate(poracle_alerter_tile_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'alerter tile {{result}}',
			},
			{
				expr: `sum by(result) (rate(poracle_alerter_geocode_total${alerterFilter}[$__rate_interval]))`,
				legendFormat: 'alerter geocode {{result}}',
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
	version: 1,
	description:
		'Concise Grafana dashboard for day-to-day PoracleNG operations, focusing on service health, flow pressure, delivery, and runtime.',
	panels: litePanels,
})

writeFileSync(new URL('./poracle-observability-dashboard.json', import.meta.url), `${JSON.stringify(dashboard, null, 2)}\n`)
writeFileSync(new URL('./poracle-operations-lite-dashboard.json', import.meta.url), `${JSON.stringify(liteDashboard, null, 2)}\n`)
