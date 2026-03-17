#!/usr/bin/env node

/**
 * One-time migration: converts existing alerter JSON config + processor TOML config
 * into the unified config/config.toml format.
 *
 * Usage: node scripts/convert-config.js [--output config/config.toml]
 */

const fs = require('fs')
const path = require('path')

const ROOT = path.resolve(__dirname, '..')
const stripJsonComments = require(path.join(ROOT, 'alerter', 'node_modules', 'strip-json-comments'))

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function readJsonc(filePath) {
	const raw = fs.readFileSync(filePath, 'utf8')
	return JSON.parse(stripJsonComments(raw))
}

function camelToSnake(str) {
	return str
		.replace(/([A-Z]+)([A-Z][a-z])/g, '$1_$2')
		.replace(/([a-z0-9])([A-Z])/g, '$1_$2')
		.toLowerCase()
}

function convertKeysToSnake(obj) {
	if (obj === null || obj === undefined) return obj
	if (Array.isArray(obj)) return obj.map(convertKeysToSnake)
	if (typeof obj !== 'object') return obj
	const result = {}
	for (const [key, value] of Object.entries(obj)) {
		result[camelToSnake(key)] = convertKeysToSnake(value)
	}
	return result
}

function deepDiff(defaults, overrides) {
	const diff = {}
	for (const [key, value] of Object.entries(overrides)) {
		const def = defaults[key]
		if (typeof value === 'object' && value !== null && !Array.isArray(value)
			&& typeof def === 'object' && def !== null && !Array.isArray(def)) {
			const sub = deepDiff(def, value)
			if (Object.keys(sub).length > 0) diff[key] = sub
		} else if (JSON.stringify(value) !== JSON.stringify(def)) {
			diff[key] = value
		}
	}
	return diff
}

// ---------------------------------------------------------------------------
// TOML writer (minimal, handles our config shapes)
// ---------------------------------------------------------------------------

function tomlValue(val) {
	if (typeof val === 'string') return JSON.stringify(val)
	if (typeof val === 'boolean') return val ? 'true' : 'false'
	if (typeof val === 'number') return String(val)
	if (Array.isArray(val)) {
		if (val.length === 0) return '[]'
		return `[${val.map(tomlValue).join(', ')}]`
	}
	return JSON.stringify(val)
}

/** Check if an array should be written as array-of-tables ([[key]]) */
function isArrayOfTables(arr) {
	return arr.length > 0 && arr.every((item) => typeof item === 'object' && item !== null && !Array.isArray(item))
}

function writeToml(obj, prefix = '') {
	let lines = []
	const tables = []
	const arrayTables = []

	for (const [key, value] of Object.entries(obj)) {
		if (value === null || value === undefined) continue
		if (Array.isArray(value) && isArrayOfTables(value)) {
			arrayTables.push([key, value])
		} else if (typeof value === 'object' && !Array.isArray(value)) {
			tables.push([key, value])
		} else {
			lines.push(`${key} = ${tomlValue(value)}`)
		}
	}

	for (const [key, value] of tables) {
		const fullKey = prefix ? `${prefix}.${key}` : key
		lines.push('')
		lines.push(`[${fullKey}]`)
		lines.push(writeToml(value, fullKey))
	}

	for (const [key, arr] of arrayTables) {
		const fullKey = prefix ? `${prefix}.${key}` : key
		for (const item of arr) {
			lines.push('')
			lines.push(`[[${fullKey}]]`)
			for (const [k, v] of Object.entries(item)) {
				if (v === null || v === undefined) continue
				lines.push(`${k} = ${tomlValue(v)}`)
			}
		}
	}

	return lines.join('\n')
}

// ---------------------------------------------------------------------------
// Obsolete fields to strip
// ---------------------------------------------------------------------------

const OBSOLETE_TUNING = new Set([
	'fast_monsters', 'disable_pokemon_cache', 'webhook_processing_workers',
	'concurrent_webhook_processors_per_worker', 'matched_processing_workers',
	'matched_worker_max_old_generation_size_mb',
])

const OBSOLETE_PVP = new Set(['data_source', 'little_league_can_evolve'])

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

function main() {
	const outputPath = process.argv.includes('--output')
		? process.argv[process.argv.indexOf('--output') + 1]
		: path.join(ROOT, 'config', 'config.toml')

	// 1. Read alerter configs
	const defaultJsonPath = path.join(ROOT, 'alerter', 'config', 'default.json')
	const localJsonPath = path.join(ROOT, 'alerter', 'config', 'local.json')

	let defaults = {}
	let local = {}

	if (fs.existsSync(defaultJsonPath)) {
		defaults = readJsonc(defaultJsonPath)
	}
	if (fs.existsSync(localJsonPath)) {
		local = readJsonc(localJsonPath)
	}

	// User overrides only
	const userOverrides = deepDiff(defaults, local)

	// 2. Read processor config
	const procConfigPath = path.join(ROOT, 'processor', 'config.toml')
	let procToml = {}
	if (fs.existsSync(procConfigPath)) {
		// Simple TOML parser for the flat processor config
		try {
			const smolToml = require(path.join(ROOT, 'alerter', 'node_modules', 'smol-toml'))
			procToml = smolToml.parse(fs.readFileSync(procConfigPath, 'utf8'))
		} catch {
			console.warn('Could not parse processor/config.toml — skipping processor overrides')
		}
	}

	// 3. Build unified config (overrides only)
	const unified = {}

	// Database from processor (DSN) or alerter overrides
	if (procToml.database?.dsn || userOverrides.database) {
		unified.database = {}
		if (procToml.database?.dsn) {
			// Parse DSN: user:pass@tcp(host:port)/database?parseTime=true
			const dsn = procToml.database.dsn
			const match = dsn.match(/^([^:]+):([^@]*)@tcp\(([^:]+):(\d+)\)\/([^?]+)/)
			if (match) {
				unified.database.user = match[1]
				unified.database.password = match[2]
				unified.database.host = match[3]
				unified.database.port = parseInt(match[4], 10)
				unified.database.database = match[5]
			}
		}
		const dbOverrides = userOverrides.database || {}
		if (dbOverrides.conn) {
			Object.assign(unified.database, convertKeysToSnake(dbOverrides.conn))
		}
		if (dbOverrides.scannerType === 'rdm') unified.database.scanner_type = 'rdm'
		if (dbOverrides.scanner) unified.database.scanner = convertKeysToSnake(dbOverrides.scanner)
	}

	// Processor networking
	const proc = {}
	if (procToml.server?.listen_addr) proc.listen_addr = procToml.server.listen_addr
	if (procToml.server?.ip_whitelist) proc.ip_whitelist = procToml.server.ip_whitelist
	if (procToml.alerter?.url) proc.alerter_url = procToml.alerter.url
	if (procToml.logging) proc.logging = procToml.logging
	if (procToml.webhookLogging) proc.webhook_logging = procToml.webhookLogging
	if (Object.keys(proc).length > 0) unified.processor = proc

	// Alerter networking
	if (userOverrides.server) {
		unified.alerter = convertKeysToSnake(userOverrides.server)
	}

	// Geofence — merge processor paths
	if (procToml.geofence?.paths || userOverrides.geofence) {
		unified.geofence = {}
		if (procToml.geofence?.paths) unified.geofence.paths = procToml.geofence.paths
		const gfOver = userOverrides.geofence || {}
		if (gfOver.path) unified.geofence.paths = Array.isArray(gfOver.path) ? gfOver.path : [gfOver.path]
		if (gfOver.defaultGeofenceName) unified.geofence.default_name = gfOver.defaultGeofenceName
		if (gfOver.defaultGeofenceColor) unified.geofence.default_color = gfOver.defaultGeofenceColor
		if (gfOver.kojiOptions) unified.geofence.koji = convertKeysToSnake(gfOver.kojiOptions)
	}

	// PVP — merge processor + alerter
	if (procToml.pvp || userOverrides.pvp) {
		unified.pvp = {}
		const pp = procToml.pvp || {}
		if (pp.level_caps) unified.pvp.level_caps = pp.level_caps
		if (pp.pvp_filter_max_rank) unified.pvp.filter_max_rank = pp.pvp_filter_max_rank
		if (pp.pvp_evolution_direct_tracking) unified.pvp.evolution_direct_tracking = pp.pvp_evolution_direct_tracking
		if (pp.pvp_filter_great_min_cp) unified.pvp.filter_great_min_cp = pp.pvp_filter_great_min_cp
		if (pp.pvp_filter_ultra_min_cp) unified.pvp.filter_ultra_min_cp = pp.pvp_filter_ultra_min_cp
		if (pp.pvp_filter_little_min_cp) unified.pvp.filter_little_min_cp = pp.pvp_filter_little_min_cp
		if (pp.include_mega_evolution) unified.pvp.include_mega_evolution = pp.include_mega_evolution

		const po = userOverrides.pvp || {}
		if (po.pvpEvolutionDirectTracking !== undefined) unified.pvp.evolution_direct_tracking = po.pvpEvolutionDirectTracking
		if (po.filterByTrack !== undefined) unified.pvp.filter_by_track = po.filterByTrack
		if (po.pvpFilterMaxRank !== undefined) unified.pvp.filter_max_rank = po.pvpFilterMaxRank
		if (po.pvpDisplayMaxRank !== undefined) unified.pvp.display_max_rank = po.pvpDisplayMaxRank
		// Strip obsolete
		for (const k of OBSOLETE_PVP) delete unified.pvp[k]
	}

	// Weather — merge processor + alerter
	if (procToml.weather || userOverrides.weather) {
		unified.weather = {}
		const pw = procToml.weather || {}
		if (pw.enable_inference !== undefined) unified.weather.enable_inference = pw.enable_inference
		if (pw.enable_change_alert !== undefined) unified.weather.change_alert = pw.enable_change_alert
		if (pw.show_altered_pokemon !== undefined) unified.weather.show_altered_pokemon = pw.show_altered_pokemon
		if (pw.show_altered_pokemon_max_count !== undefined) unified.weather.show_altered_pokemon_max_count = pw.show_altered_pokemon_max_count
		if (pw.monsters_json_path) unified.weather.monsters_json_path = pw.monsters_json_path
		if (pw.enable_forecast !== undefined) unified.weather.enable_forecast = pw.enable_forecast
		if (pw.accuweather_api_keys) unified.weather.accuweather_api_keys = pw.accuweather_api_keys
		if (pw.accuweather_day_quota) unified.weather.accuweather_day_quota = pw.accuweather_day_quota
		if (pw.forecast_refresh_interval) unified.weather.forecast_refresh_interval = pw.forecast_refresh_interval
		if (pw.local_first_fetch_hod) unified.weather.local_first_fetch_hod = pw.local_first_fetch_hod
		if (pw.smart_forecast !== undefined) unified.weather.smart_forecast = pw.smart_forecast

		const wo = userOverrides.weather || {}
		if (wo.weatherChangeAlert !== undefined) unified.weather.change_alert = wo.weatherChangeAlert
		if (wo.showAlteredPokemon !== undefined) unified.weather.show_altered_pokemon = wo.showAlteredPokemon
		if (wo.showAlteredPokemonStaticMap !== undefined) unified.weather.show_altered_pokemon_static_map = wo.showAlteredPokemonStaticMap
	}

	// Area security — merge
	if (procToml.areaSecurity || userOverrides.areaSecurity) {
		unified.area_security = {}
		const pa = procToml.areaSecurity || {}
		if (pa.enabled !== undefined) unified.area_security.enabled = pa.enabled
		if (pa.strict_locations !== undefined) unified.area_security.strict_locations = pa.strict_locations
		const ao = userOverrides.areaSecurity || {}
		if (ao.enabled !== undefined) unified.area_security.enabled = ao.enabled
		if (ao.strictLocations !== undefined) unified.area_security.strict_locations = ao.strictLocations
		if (ao.communities) unified.area_security.communities = convertKeysToSnake(ao.communities)
	}

	// Tuning — merge
	if (procToml.tuning || userOverrides.tuning) {
		unified.tuning = {}
		const pt = procToml.tuning || {}
		for (const [k, v] of Object.entries(pt)) {
			unified.tuning[k] = v
		}
		const to = userOverrides.tuning || {}
		if (to.maxDatabaseConnections !== undefined) unified.tuning.max_database_connections = to.maxDatabaseConnections
		if (to.concurrentMatchedProcessorsPerWorker !== undefined) unified.tuning.concurrent_matched_processors = to.concurrentMatchedProcessorsPerWorker
		if (to.matchedWorkerMaxQueueSize !== undefined) unified.tuning.matched_max_queue_size = to.matchedWorkerMaxQueueSize
		if (to.concurrentDiscordDestinationsPerBot !== undefined) unified.tuning.concurrent_discord_destinations = to.concurrentDiscordDestinationsPerBot
		if (to.concurrentTelegramDestinationsPerBot !== undefined) unified.tuning.concurrent_telegram_destinations = to.concurrentTelegramDestinationsPerBot
		if (to.concurrentDiscordWebhookConnections !== undefined) unified.tuning.concurrent_discord_webhooks = to.concurrentDiscordWebhookConnections
		// Strip obsolete
		for (const k of OBSOLETE_TUNING) delete unified.tuning[k]
	}

	// Simple sections: pass through user overrides as snake_case
	const simpleSections = {
		general: 'general',
		discord: 'discord',
		telegram: 'telegram',
		geocoding: 'geocoding',
		tracking: 'tracking',
		reconciliation: 'reconciliation',
		stats: 'stats',
		fallbacks: 'fallbacks',
	}

	for (const [jsonKey, tomlKey] of Object.entries(simpleSections)) {
		if (userOverrides[jsonKey] && Object.keys(userOverrides[jsonKey]).length > 0) {
			unified[tomlKey] = convertKeysToSnake(userOverrides[jsonKey])
		}
	}

	// Convert delegatedAdministration from old object-keyed format to new array-of-tables
	for (const section of ['discord', 'telegram']) {
		if (!unified[section]) continue
		const da = unified[section].delegated_administration
		if (da && typeof da === 'object') {
			const ct = da.channel_tracking
			if (ct && typeof ct === 'object' && Object.keys(ct).length > 0) {
				unified[section].delegated_admins = Object.entries(ct).map(([target, admins]) => ({ target, admins }))
			}
			if (section === 'discord') {
				const wt = da.webhook_tracking
				if (wt && typeof wt === 'object' && Object.keys(wt).length > 0) {
					unified[section].webhook_admins = Object.entries(wt).map(([target, admins]) => ({ target, admins }))
				}
			}
			// user_tracking is a flat array — rename to user_tracking_admins
			if (da.user_tracking && Array.isArray(da.user_tracking) && da.user_tracking.length > 0) {
				unified[section].user_tracking_admins = da.user_tracking
			}
			delete unified[section].delegated_administration
		}

		// Convert userRoleSubscription from guild-keyed object to array-of-tables
		const urs = unified[section].user_role_subscription
		if (urs && typeof urs === 'object' && Object.keys(urs).length > 0) {
			unified[section].role_subscriptions = Object.entries(urs).map(([guild, details]) => {
				const entry = { guild }
				if (details.roles) entry.roles = details.roles
				if (details.exclusive_roles) entry.exclusive_roles = details.exclusive_roles
				return entry
			})
			delete unified[section].user_role_subscription
		}
	}

	// Locale
	if (userOverrides.locale) {
		unified.locale = convertKeysToSnake(userOverrides.locale)
	}

	// Logger → logging
	if (userOverrides.logger) {
		unified.logging = convertKeysToSnake(userOverrides.logger)
	}

	// Alert limits
	if (userOverrides.alertLimits) {
		unified.alert_limits = convertKeysToSnake(userOverrides.alertLimits)
		// Convert limitOverride from old object-keyed format to new array-of-tables
		const lo = unified.alert_limits.limit_override
		if (lo && typeof lo === 'object' && Object.keys(lo).length > 0) {
			unified.alert_limits.overrides = Object.entries(lo).map(([target, limit]) => ({ target, limit }))
		}
		delete unified.alert_limits.limit_override
	}

	// 4. Write output
	const header = '# PoracleNG Unified Configuration\n# Generated by scripts/convert-config.js\n# Only user overrides are included — see config.example.toml for all defaults.\n'
	const tomlOutput = header + '\n' + writeToml(unified)

	fs.mkdirSync(path.dirname(outputPath), { recursive: true })
	fs.writeFileSync(outputPath, tomlOutput + '\n', 'utf8')

	console.log(`Unified config written to ${outputPath}`)
	console.log(`  Sections: ${Object.keys(unified).join(', ')}`)
}

main()
