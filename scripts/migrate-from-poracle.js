#!/usr/bin/env node

/**
 * Migrates an existing PoracleJS installation to PoracleNG.
 *
 * - Copies customized config data files (dts.json, pokemonAlias.json, etc.)
 * - Converts local.json overrides into the unified config/config.toml
 * - Warns about configuration changes the user needs to review
 *
 * Usage: node scripts/migrate-from-poracle.js
 */

const fs = require('fs')
const path = require('path')
const readline = require('readline')

const ROOT = path.resolve(__dirname, '..')
const CONFIG_DIR = path.join(ROOT, 'config')
const EXAMPLES_DIR = path.join(ROOT, 'fallbacks')

// ---------------------------------------------------------------------------
// Helpers (shared with migrate-from-poracle.js)
// ---------------------------------------------------------------------------

// Bundled JSON5 v2.2.3 (MIT) — parses JSON with comments, trailing commas, etc.
const JSON5 = require(path.join(__dirname, 'vendor-json5'))

function readJsonc(filePath) {
	const raw = fs.readFileSync(filePath, 'utf8')
	try {
		return JSON5.parse(raw)
	} catch (err) {
		const lines = raw.split('\n')
		if (err.lineNumber) {
			const lineNo = err.lineNumber
			const start = Math.max(1, lineNo - 3)
			const end = Math.min(lines.length, lineNo + 3)
			console.error(`\nParse error in ${filePath} at line ${lineNo}, column ${err.columnNumber}:`)
			for (let i = start; i <= end; i++) {
				const marker = i === lineNo ? ' >>>' : '    '
				console.error(`${marker} ${i}: ${lines[i - 1]}`)
			}
			console.error('\nCommon causes: missing comma between entries, malformed comments (use // not /)')
		}
		throw new Error(`Failed to parse ${filePath}: ${err.message}`)
	}
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

function tomlKey(key) {
	// Quote keys containing characters that aren't bare-key safe
	if (/^[A-Za-z0-9_-]+$/.test(key)) return key
	return JSON.stringify(key)
}

function tomlValue(val) {
	if (typeof val === 'string') return JSON.stringify(val)
	if (typeof val === 'boolean') return val ? 'true' : 'false'
	if (typeof val === 'number') return String(val)
	if (Array.isArray(val)) {
		if (val.length === 0) return '[]'
		return `[${val.map(tomlValue).join(', ')}]`
	}
	if (typeof val === 'object' && val !== null) {
		// Inline table for nested objects with arbitrary keys (e.g. Discord IDs)
		const pairs = Object.entries(val)
			.filter(([, v]) => v !== null && v !== undefined)
			.map(([k, v]) => `${tomlKey(k)} = ${tomlValue(v)}`)
		return `{ ${pairs.join(', ')} }`
	}
	return JSON.stringify(val)
}

/** Check if an object's own keys are all valid bare TOML config keys */
function hasOnlyConfigKeys(obj) {
	for (const k of Object.keys(obj)) {
		if (!/^[A-Za-z_][A-Za-z0-9_-]*$/.test(k)) return false
	}
	return true
}

/** Check if an array should be written as array-of-tables ([[key]]) */
function isArrayOfTables(arr) {
	return arr.length > 0 && arr.every((item) => typeof item === 'object' && item !== null && !Array.isArray(item))
}

function writeToml(obj, prefix = '') {
	const lines = []
	const tables = []
	const arrayTables = []

	for (const [key, value] of Object.entries(obj)) {
		if (value === null || value === undefined) continue
		if (Array.isArray(value) && isArrayOfTables(value)) {
			// Array of tables — write as [[key]] sections
			arrayTables.push([key, value])
		} else if (typeof value === 'object' && !Array.isArray(value) && hasOnlyConfigKeys(value)) {
			// This object has config-like keys — write as a TOML table section
			tables.push([key, value])
		} else {
			// Primitives, arrays, or objects with arbitrary keys — write as key = value
			lines.push(`${tomlKey(key)} = ${tomlValue(value)}`)
		}
	}

	for (const [key, value] of tables) {
		const fullKey = prefix ? `${prefix}.${tomlKey(key)}` : tomlKey(key)
		// Check if this table has any direct key-value pairs (not just sub-tables)
		const hasDirect = Object.values(value).some((v) => v !== null && v !== undefined && (typeof v !== 'object' || Array.isArray(v) || !hasOnlyConfigKeys(v)))
		const content = writeToml(value, fullKey)
		if (hasDirect) {
			lines.push('')
			lines.push(`[${fullKey}]`)
		}
		lines.push(content)
	}

	for (const [key, arr] of arrayTables) {
		const fullKey = prefix ? `${prefix}.${tomlKey(key)}` : tomlKey(key)
		for (const item of arr) {
			lines.push('')
			lines.push(`[[${fullKey}]]`)
			for (const [k, v] of Object.entries(item)) {
				if (v === null || v === undefined) continue
				lines.push(`${tomlKey(k)} = ${tomlValue(v)}`)
			}
		}
	}

	return lines.join('\n')
}

// ---------------------------------------------------------------------------
// File comparison
// ---------------------------------------------------------------------------

function filesAreEqual(fileA, fileB) {
	try {
		const a = fs.readFileSync(fileA)
		const b = fs.readFileSync(fileB)
		return a.equals(b)
	} catch {
		return false
	}
}

function jsonFilesAreEqual(fileA, fileB) {
	try {
		const a = readJsonc(fileA)
		const b = readJsonc(fileB)
		return JSON.stringify(a) === JSON.stringify(b)
	} catch {
		return false
	}
}

// ---------------------------------------------------------------------------
// Config conversion (from migrate-from-poracle.js)
// ---------------------------------------------------------------------------

const OBSOLETE_TUNING = new Set([
	'fast_monsters', 'disable_pokemon_cache', 'webhook_processing_workers',
	'concurrent_webhook_processors_per_worker', 'matched_processing_workers',
	'matched_worker_max_old_generation_size_mb',
])

const OBSOLETE_PVP = new Set(['data_source', 'little_league_can_evolve'])

function buildUnifiedConfig(defaults, local) {
	const userOverrides = deepDiff(defaults, local)
	const unified = {}

	// Database — always include full connection parameters (required)
	const dbConn = (local.database && local.database.conn) || (defaults.database && defaults.database.conn) || {}
	unified.database = {
		host: dbConn.host || '127.0.0.1',
		port: dbConn.port || 3306,
		user: dbConn.user || 'poracleuser',
		password: dbConn.password || 'poraclepassword',
		database: dbConn.database || 'poracle',
	}
	const scannerType = (local.database && local.database.scannerType) || (defaults.database && defaults.database.scannerType)
	const scanner = (local.database && local.database.scanner) || (userOverrides.database && userOverrides.database.scanner)
	if (scanner) unified.database.scanner = convertKeysToSnake(scanner)
	if (scannerType === 'rdm') {
		if (!unified.database.scanner) unified.database.scanner = {}
		unified.database.scanner.type = 'rdm'
	}

	// Networking — processor takes the old Poracle port
	const serverOverrides = userOverrides.server || {}
	const oldPort = parseInt((local.server && local.server.port) || (defaults.server && defaults.server.port) || '3030', 10)

	unified.processor = {
		port: oldPort,
	}

	// Preserve api_secret from server overrides
	if (serverOverrides.apiSecret) {
		unified.processor.api_secret = serverOverrides.apiSecret
	}

	// Geofence — always include paths so both components find the geofence
	unified.geofence = {}
	const gfOver = userOverrides.geofence || {}
	const gfPaths = gfOver.path ? (Array.isArray(gfOver.path) ? gfOver.path : [gfOver.path]) : ['geofences/geofence.json']
	unified.geofence.paths = gfPaths
	if (gfOver.defaultGeofenceName) unified.geofence.default_name = gfOver.defaultGeofenceName
	if (gfOver.defaultGeofenceColor) unified.geofence.default_color = gfOver.defaultGeofenceColor
	if (gfOver.kojiOptions) unified.geofence.koji = convertKeysToSnake(gfOver.kojiOptions)

	// PVP
	if (userOverrides.pvp) {
		unified.pvp = {}
		const po = userOverrides.pvp
		if (po.pvpEvolutionDirectTracking !== undefined) unified.pvp.evolution_direct_tracking = po.pvpEvolutionDirectTracking
		if (po.filterByTrack !== undefined) unified.pvp.filter_by_track = po.filterByTrack
		if (po.pvpFilterMaxRank !== undefined) unified.pvp.filter_max_rank = po.pvpFilterMaxRank
		if (po.pvpDisplayMaxRank !== undefined) unified.pvp.display_max_rank = po.pvpDisplayMaxRank
		if (po.pvpFilterGreatMinCP !== undefined) unified.pvp.filter_great_min_cp = po.pvpFilterGreatMinCP
		if (po.pvpFilterUltraMinCP !== undefined) unified.pvp.filter_ultra_min_cp = po.pvpFilterUltraMinCP
		if (po.pvpFilterLittleMinCP !== undefined) unified.pvp.filter_little_min_cp = po.pvpFilterLittleMinCP
		if (po.pvpDisplayGreatMinCP !== undefined) unified.pvp.display_great_min_cp = po.pvpDisplayGreatMinCP
		if (po.pvpDisplayUltraMinCP !== undefined) unified.pvp.display_ultra_min_cp = po.pvpDisplayUltraMinCP
		if (po.pvpDisplayLittleMinCP !== undefined) unified.pvp.display_little_min_cp = po.pvpDisplayLittleMinCP
		if (po.includeMegaEvolution !== undefined) unified.pvp.include_mega_evolution = po.includeMegaEvolution
		if (po.levelCaps) unified.pvp.level_caps = po.levelCaps
		for (const k of OBSOLETE_PVP) delete unified.pvp[k]
	}

	// Weather
	if (userOverrides.weather) {
		unified.weather = {}
		const wo = userOverrides.weather
		if (wo.weatherChangeAlert !== undefined) unified.weather.change_alert = wo.weatherChangeAlert
		if (wo.showAlteredPokemon !== undefined) unified.weather.show_altered_pokemon = wo.showAlteredPokemon
		if (wo.showAlteredPokemonStaticMap !== undefined) unified.weather.show_altered_pokemon_static_map = wo.showAlteredPokemonStaticMap
		if (wo.showAlteredPokemonMaxCount !== undefined) unified.weather.show_altered_pokemon_max_count = wo.showAlteredPokemonMaxCount
		if (wo.enableWeatherForecast !== undefined) unified.weather.enable_forecast = wo.enableWeatherForecast
		if (wo.apiKeyAccuWeather) unified.weather.accuweather_api_keys = wo.apiKeyAccuWeather
		if (wo.apiKeyDayQuota) unified.weather.accuweather_day_quota = wo.apiKeyDayQuota
		if (wo.forecastRefreshInterval) unified.weather.forecast_refresh_interval = wo.forecastRefreshInterval
		if (wo.localFirstFetchHOD) unified.weather.local_first_fetch_hod = wo.localFirstFetchHOD
		if (wo.smartForecast !== undefined) unified.weather.smart_forecast = wo.smartForecast
	}

	// Area security
	if (userOverrides.areaSecurity) {
		unified.area_security = {}
		const ao = userOverrides.areaSecurity
		if (ao.enabled !== undefined) unified.area_security.enabled = ao.enabled
		if (ao.strictLocations !== undefined) unified.area_security.strict_locations = ao.strictLocations
		if (ao.communities) {
			// Convert to array-of-tables format with name field
			unified.area_security.communities = []
			for (const [name, community] of Object.entries(ao.communities)) {
				unified.area_security.communities.push({ name, ...convertKeysToSnake(community) })
			}
		}
	}

	// Tuning
	if (userOverrides.tuning) {
		unified.tuning = {}
		const to = userOverrides.tuning
		if (to.maxDatabaseConnections !== undefined) unified.tuning.max_database_connections = to.maxDatabaseConnections
		if (to.concurrentMatchedProcessorsPerWorker !== undefined) unified.tuning.concurrent_matched_processors = to.concurrentMatchedProcessorsPerWorker
		if (to.matchedWorkerMaxQueueSize !== undefined) unified.tuning.matched_max_queue_size = to.matchedWorkerMaxQueueSize
		if (to.concurrentDiscordDestinationsPerBot !== undefined) unified.tuning.concurrent_discord_destinations = to.concurrentDiscordDestinationsPerBot
		if (to.concurrentTelegramDestinationsPerBot !== undefined) unified.tuning.concurrent_telegram_destinations = to.concurrentTelegramDestinationsPerBot
		if (to.concurrentDiscordWebhookConnections !== undefined) unified.tuning.concurrent_discord_webhooks = to.concurrentDiscordWebhookConnections
		for (const k of OBSOLETE_TUNING) delete unified.tuning[k]
	}

	// Simple sections
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

	// Logger -> logging
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

	// Remove empty sections
	for (const key of Object.keys(unified)) {
		const val = unified[key]
		if (typeof val === 'object' && val !== null && !Array.isArray(val) && Object.keys(val).length === 0) {
			delete unified[key]
		}
	}

	return unified
}

// ---------------------------------------------------------------------------
// Interactive prompts
// ---------------------------------------------------------------------------

function ask(rl, question) {
	return new Promise((resolve) => rl.question(question, resolve))
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main() {
	const rl = readline.createInterface({ input: process.stdin, output: process.stdout })

	console.log()
	console.log('=== PoracleNG Migration Tool ===')
	console.log()
	console.log('This script migrates your existing PoracleJS configuration to PoracleNG.')
	console.log('It will:')
	console.log('  1. Copy any customized data files (DTS, aliases, etc.) to config/')
	console.log('  2. Convert your local.json overrides into config/config.toml')
	console.log('  3. Show you what manual changes are needed')
	console.log()

	const oldPath = (await ask(rl, 'Path to your existing PoracleJS installation: ')).trim()

	if (!oldPath) {
		console.error('No path provided.')
		rl.close()
		process.exit(1)
	}

	const resolvedPath = path.resolve(oldPath)
	const oldConfigDir = path.join(resolvedPath, 'config')
	const oldDefaultsDir = path.join(oldConfigDir, 'defaults')

	// Validate it looks like PoracleJS
	if (!fs.existsSync(path.join(oldConfigDir, 'default.json'))) {
		console.error(`Error: ${oldConfigDir}/default.json not found.`)
		console.error('This does not look like a PoracleJS installation.')
		rl.close()
		process.exit(1)
	}

	console.log()
	console.log(`Found PoracleJS at: ${resolvedPath}`)
	console.log()

	// Ensure target directories exist
	fs.mkdirSync(CONFIG_DIR, { recursive: true })

	const copied = []
	const skipped = []

	// -------------------------------------------------------------------
	// 1. Copy customized data files
	// -------------------------------------------------------------------

	const dataFiles = [
		'dts.json',
		'pokemonAlias.json',
		'partials.json',
		'testdata.json',
		'broadcast.json',
		'channelTemplate.json',
		'emoji.json',
	]

	console.log('Checking data files...')

	for (const file of dataFiles) {
		const oldFile = path.join(oldConfigDir, file)
		if (!fs.existsSync(oldFile)) {
			skipped.push(`${file} (not found)`)
			continue
		}

		// Check if it matches the old defaults
		const oldDefault = path.join(oldDefaultsDir, file)
		if (fs.existsSync(oldDefault) && jsonFilesAreEqual(oldFile, oldDefault)) {
			skipped.push(`${file} (unchanged from defaults)`)
			continue
		}

		// Also check against our bundled examples
		const exampleFile = path.join(EXAMPLES_DIR, file)
		if (fs.existsSync(exampleFile) && jsonFilesAreEqual(oldFile, exampleFile)) {
			skipped.push(`${file} (matches bundled example)`)
			continue
		}

		const dest = path.join(CONFIG_DIR, file)
		if (fs.existsSync(dest)) {
			const overwrite = await ask(rl, `  ${file} already exists in config/. Overwrite? [y/N] `)
			if (overwrite.trim().toLowerCase() !== 'y') {
				skipped.push(`${file} (already exists, kept)`)
				continue
			}
		}

		fs.copyFileSync(oldFile, dest)
		copied.push(file)
		console.log(`  Copied: ${file}`)
	}

	// Copy DTS subdirectory (additional DTS files)
	const oldDtsDir = path.join(oldConfigDir, 'dts')
	if (fs.existsSync(oldDtsDir)) {
		const dtsFiles = fs.readdirSync(oldDtsDir).filter((f) => fs.statSync(path.join(oldDtsDir, f)).isFile())
		if (dtsFiles.length > 0) {
			const destDtsDir = path.join(CONFIG_DIR, 'dts')
			fs.mkdirSync(destDtsDir, { recursive: true })
			for (const f of dtsFiles) {
				fs.copyFileSync(path.join(oldDtsDir, f), path.join(destDtsDir, f))
				copied.push(`dts/${f}`)
				console.log(`  Copied: dts/${f}`)
			}
		}
	}

	// Copy customMaps directory
	const oldCustomMapsDir = path.join(oldConfigDir, 'customMaps')
	if (fs.existsSync(oldCustomMapsDir)) {
		const mapFiles = fs.readdirSync(oldCustomMapsDir).filter((f) => f.endsWith('.json'))
		if (mapFiles.length > 0) {
			const destMapsDir = path.join(CONFIG_DIR, 'customMaps')
			fs.mkdirSync(destMapsDir, { recursive: true })
			for (const f of mapFiles) {
				fs.copyFileSync(path.join(oldCustomMapsDir, f), path.join(destMapsDir, f))
				copied.push(`customMaps/${f}`)
				console.log(`  Copied: customMaps/${f}`)
			}
		}
	}

	// Copy geofence files
	const oldGeofence = path.join(oldConfigDir, 'geofence.json')
	if (fs.existsSync(oldGeofence)) {
		const oldGeoDefault = path.join(oldDefaultsDir, 'geofence.json')
		const isDefault = fs.existsSync(oldGeoDefault) && jsonFilesAreEqual(oldGeofence, oldGeoDefault)

		if (!isDefault) {
			const destGeofencesDir = path.join(CONFIG_DIR, 'geofences')
			fs.mkdirSync(destGeofencesDir, { recursive: true })
			const dest = path.join(destGeofencesDir, 'geofence.json')
			if (fs.existsSync(dest)) {
				const overwrite = await ask(rl, '  geofences/geofence.json already exists. Overwrite? [y/N] ')
				if (overwrite.trim().toLowerCase() === 'y') {
					fs.copyFileSync(oldGeofence, dest)
					copied.push('geofences/geofence.json')
					console.log('  Copied: geofences/geofence.json')
				} else {
					skipped.push('geofences/geofence.json (already exists, kept)')
				}
			} else {
				fs.copyFileSync(oldGeofence, dest)
				copied.push('geofences/geofence.json')
				console.log('  Copied: geofences/geofence.json')
			}
		} else {
			skipped.push('geofence.json (empty default)')
		}
	}

	// Custom locale translations
	const customLocaleFiles = fs.readdirSync(oldConfigDir)
		.filter((f) => f.startsWith('custom.') && f.endsWith('.json'))
	for (const f of customLocaleFiles) {
		const dest = path.join(CONFIG_DIR, f)
		if (fs.existsSync(dest)) {
			const overwrite = await ask(rl, `  ${f} already exists in config/. Overwrite? [y/N] `)
			if (overwrite.trim().toLowerCase() !== 'y') {
				skipped.push(`${f} (already exists, kept)`)
				continue
			}
		}
		fs.copyFileSync(path.join(oldConfigDir, f), dest)
		copied.push(f)
		console.log(`  Copied: ${f}`)
	}

	// -------------------------------------------------------------------
	// 2. Convert local.json → config.toml
	// -------------------------------------------------------------------

	console.log()
	console.log('Converting configuration...')

	const defaults = readJsonc(path.join(oldConfigDir, 'default.json'))
	const localJsonPath = path.join(oldConfigDir, 'local.json')
	const local = fs.existsSync(localJsonPath) ? readJsonc(localJsonPath) : defaults

	const unified = buildUnifiedConfig(defaults, local)

	// Fix geofence paths for new layout (now relative to config/ directory)
	if (unified.geofence?.paths) {
		unified.geofence.paths = unified.geofence.paths.map((p) => {
			if (p === 'config/geofence.json' || p === './config/geofence.json') return 'geofences/geofence.json'
			// Strip leading config/ or ./config/ since paths are now relative to config dir
			return p.replace(/^\.?\/?(config\/)?/, '')
		})
	}

	const outputPath = path.join(CONFIG_DIR, 'config.toml')
	if (fs.existsSync(outputPath)) {
		const overwrite = await ask(rl, 'config/config.toml already exists. Overwrite? [y/N] ')
		if (overwrite.trim().toLowerCase() !== 'y') {
			console.log('  Skipped config.toml (kept existing)')
			rl.close()
			printSummary(copied, skipped, unified)
			return
		}
	}

	const header = [
		'# PoracleNG Unified Configuration',
		`# Migrated from ${resolvedPath}`,
		`# Generated on ${new Date().toISOString().slice(0, 10)}`,
		'# Only your overrides are included - see config.example.toml for all defaults.',
	].join('\n')

	const tomlOutput = header + '\n\n' + writeToml(unified) + '\n'
	fs.writeFileSync(outputPath, tomlOutput, 'utf8')
	console.log(`  Written: config/config.toml`)

	rl.close()
	printSummary(copied, skipped, unified)
}

function printSummary(copied, skipped, unified) {
	console.log()
	console.log('=== Migration Summary ===')
	console.log()

	if (copied.length > 0) {
		console.log(`Copied ${copied.length} file(s):`)
		for (const f of copied) console.log(`  + ${f}`)
	} else {
		console.log('No customized data files to copy (all were defaults).')
	}

	if (skipped.length > 0) {
		console.log()
		console.log(`Skipped ${skipped.length} file(s):`)
		for (const f of skipped) console.log(`  - ${f}`)
	}

	console.log()
	console.log('=== IMPORTANT: Review Required ===')
	console.log()
	console.log('PoracleNG is a single Go process that handles everything:')
	console.log(`  - Receives webhooks, matches alerts, renders templates, sends messages`)
	console.log(`  - Discord bot (commands, reconciliation)`)
	console.log(`  - Telegram bot (commands, reconciliation)`)
	console.log(`  - All API endpoints (tracking, humans, profiles, config, masterdata)`)
	console.log()
	console.log(`Listening on port :${unified.processor?.port || 3030}`)
	console.log()
	console.log(`Your webhook sender (e.g. Golbat) should POST to port :${unified.processor?.port || 3030}.`)
	console.log()

	// Check for scanner type issues
	const scannerType = unified.database?.scanner?.type || unified.database?.scanner_type
	if (scannerType === 'mad') {
		console.log('WARNING: Your config had scanner_type = "mad". MAD is no longer')
		console.log('supported. The default scanner type is now "golbat". If you are')
		console.log('using RDM, add type = "rdm" under [database.scanner].')
		console.log()
	}

	// Remind about geofence paths
	console.log('Geofence files should be placed in config/geofences/ and referenced')
	console.log('as "geofences/<filename>" in config.toml (paths are relative to config/).')
	console.log()
	console.log('See config/config.example.toml for the full list of available settings.')
	console.log()
}

main().catch((err) => {
	console.error('Migration failed:', err.message)
	process.exit(1)
})
