/**
 * Adapts a parsed TOML config (snake_case) into the camelCase object shape
 * that the alerter codebase expects (matching default.json structure).
 */

function snakeToCamel(str) {
	return str.replace(/_([a-z0-9])/g, (_, c) => c.toUpperCase())
}

function convertKeys(obj) {
	if (obj === null || obj === undefined) return obj
	if (Array.isArray(obj)) return obj.map(convertKeys)
	if (typeof obj !== 'object') return obj
	const result = {}
	for (const [key, value] of Object.entries(obj)) {
		result[snakeToCamel(key)] = convertKeys(value)
	}
	return result
}

function adaptConfig(toml) {
	const config = {}

	// server — from [alerter] section
	const alerter = toml.alerter || {}
	config.server = {
		host: alerter.host || '127.0.0.1',
		port: String(alerter.port || 3030),
		ipWhitelist: alerter.ip_whitelist || [],
		ipBlacklist: alerter.ip_blacklist || [],
		apiSecret: alerter.api_secret || '',
	}

	// processor — from [alerter] section
	config.processor = {
		url: alerter.processor_url || '',
	}

	// database — from [database] section
	const db = toml.database || {}
	config.database = {
		client: 'mysql',
		conn: {
			host: db.host || '127.0.0.1',
			port: db.port || 3306,
			user: db.user || 'poracleuser',
			password: db.password || 'poraclepassword',
			database: db.database || 'poracle',
		},
		scannerType: db.scanner_type || 'none',
	}
	if (db.scanner) {
		config.database.scanner = {
			host: db.scanner.host || '127.0.0.1',
			port: db.scanner.port || 3306,
			user: db.scanner.user || 'scanneruser',
			password: db.scanner.password || 'scannerpassword',
			database: db.scanner.database || 'scannerdb',
		}
	}

	// locale
	config.locale = convertKeys(toml.locale || {})
	if (!config.locale.timeformat) config.locale.timeformat = 'en-gb'
	if (!config.locale.time) config.locale.time = 'LTS'
	if (!config.locale.date) config.locale.date = 'L'
	if (!config.locale.addressFormat) config.locale.addressFormat = '{{{streetName}}} {{streetNumber}}'
	if (!config.locale.language) config.locale.language = 'en'

	// geofence
	const gf = toml.geofence || {}
	config.geofence = {
		path: gf.paths || ['config/geofence.json'],
		defaultGeofenceName: gf.default_name || 'city',
		defaultGeofenceColor: gf.default_color || '#3399ff',
		kojiOptions: convertKeys(gf.koji || {}),
	}

	// weather
	const w = toml.weather || {}
	config.weather = {
		weatherChangeAlert: w.change_alert !== undefined ? w.change_alert : false,
		showAlteredPokemon: w.show_altered_pokemon !== undefined ? w.show_altered_pokemon : false,
		showAlteredPokemonStaticMap: w.show_altered_pokemon_static_map !== undefined ? w.show_altered_pokemon_static_map : false,
		showAlteredPokemonMaxCount: w.show_altered_pokemon_max_count !== undefined ? w.show_altered_pokemon_max_count : 10,
		enableWeatherForecast: w.enable_forecast !== undefined ? w.enable_forecast : false,
		apiKeyAccuWeather: w.accuweather_api_keys || [''],
		apiKeyDayQuota: w.accuweather_day_quota || 50,
		smartForecast: w.smart_forecast !== undefined ? w.smart_forecast : false,
		localFirstFetchHOD: w.local_first_fetch_hod || 3,
		forecastRefreshInterval: w.forecast_refresh_interval || 8,
	}

	// pvp
	const p = toml.pvp || {}
	config.pvp = {
		dataSource: 'webhook',
		levelCaps: p.level_caps || [50],
		includeMegaEvolution: p.include_mega_evolution !== undefined ? p.include_mega_evolution : false,
		pvpEvolutionDirectTracking: p.evolution_direct_tracking !== undefined ? p.evolution_direct_tracking : false,
		filterByTrack: p.filter_by_track !== undefined ? p.filter_by_track : false,
		pvpFilterMaxRank: p.filter_max_rank || 10,
		pvpFilterGreatMinCP: p.filter_great_min_cp || 1400,
		pvpFilterUltraMinCP: p.filter_ultra_min_cp || 2350,
		pvpFilterLittleMinCP: p.filter_little_min_cp || 450,
		pvpDisplayMaxRank: p.display_max_rank || 10,
		pvpDisplayGreatMinCP: p.display_great_min_cp || 1400,
		pvpDisplayUltraMinCP: p.display_ultra_min_cp || 2350,
		pvpDisplayLittleMinCP: p.display_little_min_cp || 450,
	}

	// area_security → areaSecurity
	const as = toml.area_security || {}
	config.areaSecurity = {
		enabled: as.enabled !== undefined ? as.enabled : false,
		strictLocations: as.strict_locations !== undefined ? as.strict_locations : false,
		communities: convertKeys(as.communities || {}),
	}

	// Simple sections: convert snake_case keys directly
	config.general = convertKeys(toml.general || {})
	config.discord = convertKeys(toml.discord || {})
	config.telegram = convertKeys(toml.telegram || {})
	config.geocoding = convertKeys(toml.geocoding || {})
	config.tracking = convertKeys(toml.tracking || {})
	config.reconciliation = convertKeys(toml.reconciliation || {})
	config.stats = convertKeys(toml.stats || {})
	config.fallbacks = convertKeys(toml.fallbacks || {})
	config.alertLimits = convertKeys(toml.alert_limits || {})

	// logger — from [logging] section
	const lg = toml.logging || {}
	config.logger = {
		consoleLogLevel: lg.console_log_level || 'verbose',
		logLevel: lg.log_level || 'verbose',
		enableLogs: convertKeys(lg.enable_logs || {}),
		timingStats: lg.timing_stats !== undefined ? lg.timing_stats : false,
		dailyLogLimit: lg.daily_log_limit || 7,
		webhookLogLimit: lg.webhook_log_limit || 12,
	}

	// tuning
	const t = toml.tuning || {}
	config.tuning = {
		fastMonsters: true,
		disablePokemonCache: true,
		maxDatabaseConnections: t.max_database_connections || 15,
		concurrentMatchedProcessorsPerWorker: t.concurrent_matched_processors || 10,
		matchedWorkerMaxQueueSize: t.matched_max_queue_size || 5000,
		concurrentDiscordDestinationsPerBot: t.concurrent_discord_destinations || 10,
		concurrentTelegramDestinationsPerBot: t.concurrent_telegram_destinations || 10,
		concurrentDiscordWebhookConnections: t.concurrent_discord_webhooks || 10,
	}

	return config
}

module.exports = { adaptConfig, snakeToCamel, convertKeys }
