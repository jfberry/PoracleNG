/**
 * Adapts a parsed TOML config (snake_case) into the camelCase object shape
 * that the alerter codebase expects (matching default.json structure).
 *
 * Every field from default.json has a default here so the alerter works
 * with a minimal config.toml containing only user overrides.
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

/** Merge defaults into target — only sets keys that are missing in target */
function defaults(target, defs) {
	for (const [key, value] of Object.entries(defs)) {
		if (target[key] === undefined) target[key] = value
	}
	return target
}

function adaptConfig(toml) {
	const config = {}

	// ---- server (from [alerter] section) ----
	const alerter = toml.alerter || {}
	config.server = {
		host: alerter.host || '127.0.0.1',
		port: String(alerter.port || 3031),
		ipWhitelist: alerter.ip_whitelist || [],
		ipBlacklist: alerter.ip_blacklist || [],
		apiSecret: alerter.api_secret || '',
	}

	// ---- processor ----
	if (!alerter.processor_url) {
		throw new Error('Config error: [alerter] processor_url is required (e.g. "http://localhost:3030")')
	}
	config.processor = {
		url: alerter.processor_url,
	}

	// ---- database ----
	const db = toml.database || {}
	if (!db.user || !db.database) {
		throw new Error('Config error: [database] user and database are required')
	}
	config.database = {
		client: 'mysql',
		conn: {
			host: db.host || '127.0.0.1',
			port: db.port || 3306,
			user: db.user || 'poracleuser',
			password: db.password || 'poraclepassword',
			database: db.database || 'poracle',
		},
		scannerType: db.scanner_type || 'golbat',
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

	// ---- general ----
	const gen = toml.general || {}
	config.general = convertKeys(gen)
	defaults(config.general, {
		environment: 'production',
		alertMinimumTime: 120,
		ignoreLongRaids: false,
		imgUrl: 'https://raw.githubusercontent.com/nileplumb/PkmnShuffleMap/master/UICONS',
		imgUrlAlt: '',
		stickerUrl: 'https://raw.githubusercontent.com/bbdoc/tgUICONS/main/Shuffle',
		images: {},
		stickers: {},
		requestShinyImages: false,
		populatePokestopName: false,
		locale: 'en',
		disabledCommands: [],
		disablePokemon: false,
		disableRaid: false,
		disablePokestop: false,
		disableInvasion: false,
		disableLure: false,
		disableQuest: false,
		disableWeather: false,
		disableNest: false,
		disableGym: false,
		disableFortUpdate: false,
		processConfirmedInvasionLineups: false,
		disableUnconfirmedInvasion: false,
		roleCheckMode: 'ignore',
		availableLanguages: {},
		defaultTemplateName: 1,
		dtsDictionary: {},
		shortlinkProvider: 'hideuri',
		shortlinkProviderURL: '',
		shortlinkProviderKey: '',
		shortlinkProviderDomain: '',
		rdmURL: '',
		reactMapURL: '',
		rocketMadURL: '',
	})

	// ---- locale ----
	config.locale = convertKeys(toml.locale || {})
	defaults(config.locale, {
		timeformat: 'en-gb',
		time: 'LTS',
		date: 'L',
		addressFormat: '{{{streetName}}} {{streetNumber}}',
		language: 'en',
	})

	// ---- geofence ----
	const gf = toml.geofence || {}
	config.geofence = {
		path: gf.paths || ['geofences/geofence.json'],
		defaultGeofenceName: gf.default_name || 'city',
		defaultGeofenceColor: gf.default_color || '#3399ff',
		kojiOptions: convertKeys(gf.koji || {}),
	}
	defaults(config.geofence.kojiOptions, {
		bearerToken: '',
	})

	// ---- weather ----
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

	// ---- pvp ----
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

	// ---- tracking ----
	config.tracking = convertKeys(toml.tracking || {})
	defaults(config.tracking, {
		everythingFlagPermissions: 'allow-any',
		defaultDistance: 0,
		maxDistance: 0,
		enableGymBattle: false,
		defaultUserTrackingLevelCap: 0,
	})

	// ---- reconciliation ----
	config.reconciliation = convertKeys(toml.reconciliation || {})
	defaults(config.reconciliation, {
		discord: {
			updateUserNames: false,
			removeInvalidUsers: true,
			registerNewUsers: false,
			updateChannelNames: true,
			updateChannelNotes: false,
			unregisterMissingChannels: false,
		},
		telegram: {
			updateUserNames: false,
			removeInvalidUsers: true,
		},
	})

	// ---- areaSecurity ----
	const as = toml.area_security || {}
	config.areaSecurity = {
		enabled: as.enabled !== undefined ? as.enabled : false,
		strictLocations: as.strict_locations !== undefined ? as.strict_locations : false,
		communities: convertKeys(as.communities || {}),
	}

	// ---- discord ----
	config.discord = convertKeys(toml.discord || {})
	defaults(config.discord, {
		enabled: true,
		activity: 'PoracleJS',
		workerStatus: 'invisible',
		workerActivity: 'PoracleJS Helper',
		disableAutoGreetings: false,
		uploadEmbedImages: false,
		checkRole: false,
		checkRoleInterval: 6,
		token: [''],
		guilds: [''],
		channels: [''],
		userRole: [''],
		admins: [''],
		delegatedAdministration: {},
		commandSecurity: {},
		prefix: '!',
		ivColors: ['#9D9D9D', '#FFFFFF', '#1EFF00', '#0070DD', '#A335EE', '#FF8000'],
		dmLogChannelID: '',
		dmLogChannelDeletionTime: 0,
		messageDeleteDelay: 0,
		unrecognisedCommandMessage: '',
		unregisteredUserMessage: '',
		lostRoleMessage: '',
	})
	// Auto-detect enabled from token presence if not explicitly set in TOML
	if ((toml.discord || {}).enabled === undefined) {
		config.discord.enabled = !!(config.discord.token && config.discord.token.length && config.discord.token[0])
	}

	// ---- telegram ----
	config.telegram = convertKeys(toml.telegram || {})
	defaults(config.telegram, {
		enabled: false,
		token: '',
		admins: [''],
		delegatedAdministration: {},
		channels: [''],
		groupWelcomeText: 'Welcome {user}, remember to click on me and \'start bot\' to be able to receive messages',
		botWelcomeText: 'You are now registered with Poracle',
		botGoodbyeMessage: '',
		unregisteredUserMessage: '',
		unrecognisedCommandMessage: '',
		checkRole: false,
		checkRoleInterval: 6,
		registerOnStart: false,
	})

	// ---- geocoding (special handling for URL-suffix keys: providerURL not providerUrl) ----
	const geo = toml.geocoding || {}
	config.geocoding = {
		provider: geo.provider || 'none',
		providerURL: geo.provider_url || '',
		forwardOnly: geo.forward_only !== undefined ? geo.forward_only : false,
		cacheDetail: geo.cache_detail || 3,
		dayStyle: geo.day_style || '',
		dawnStyle: geo.dawn_style || '',
		duskStyle: geo.dusk_style || '',
		nightStyle: geo.night_style || '',
		intersectionUsers: geo.intersection_users || [],
		staticProvider: geo.static_provider || 'none',
		staticProviderURL: geo.static_provider_url || '',
		tileserverSettings: convertKeys(geo.tileserver_settings || {
			default: {
				type: 'staticMap',
				width: 500,
				height: 250,
				zoom: 15,
				pregenerate: true,
				includeStops: false,
			},
		}),
		geocodingKey: geo.geocoding_key || [''],
		staticKey: geo.static_key || [''],
		width: geo.width || 320,
		height: geo.height || 200,
		zoom: geo.zoom || 15,
		spriteHeight: geo.sprite_height || 20,
		spriteWidth: geo.sprite_width || 20,
		scale: geo.scale || 2,
		type: geo.type || 'klokantech-basic',
	}

	// ---- stats ----
	config.stats = convertKeys(toml.stats || {})
	defaults(config.stats, {
		maxPokemonId: 1010,
		minSampleSize: 10000,
		pokemonCountToKeep: 8,
		rarityRefreshInterval: 5,
		rarityGroup2Uncommon: 1,
		rarityGroup3Rare: 0.5,
		rarityGroup4VeryRare: 0.03,
		rarityGroup5UltraRare: 0.01,
		excludeFromRare: [],
	})

	// ---- fallbacks ----
	config.fallbacks = convertKeys(toml.fallbacks || {})
	defaults(config.fallbacks, {
		staticMap: 'https://raw.githubusercontent.com/KartulUdus/PoracleJS/images/fallback/staticMap.png',
		imgUrl: 'https://raw.githubusercontent.com/KartulUdus/PoracleJS/images/fallback/mon.png',
		imgUrlWeather: 'https://raw.githubusercontent.com/KartulUdus/PoracleJS/images/fallback/weather.png',
		imgUrlEgg: 'https://raw.githubusercontent.com/KartulUdus/PoracleJS/images/fallback/uni.png',
		imgUrlGym: 'https://raw.githubusercontent.com/KartulUdus/PoracleJS/images/fallback/gym.png',
		imgUrlPokestop: 'https://raw.githubusercontent.com/KartulUdus/PoracleJS/images/fallback/pokestop.png',
		pokestopUrl: 'https://raw.githubusercontent.com/KartulUdus/PoracleJS/images/fallback/pokestop.png',
	})

	// ---- alertLimits ----
	config.alertLimits = convertKeys(toml.alert_limits || {})
	defaults(config.alertLimits, {
		timingPeriod: 240,
		dmLimit: 20,
		channelLimit: 40,
		maxLimitsBeforeStop: 10,
		disableOnStop: false,
		shameChannel: '',
		limitOverride: {},
	})

	// ---- logger (from [logging] section) ----
	const lg = toml.logging || {}
	const enableLogs = convertKeys(lg.enable_logs || {})
	defaults(enableLogs, {
		webhooks: false,
		discord: true,
		telegram: true,
		pvp: false,
	})
	config.logger = {
		consoleLogLevel: lg.level || 'verbose',
		logLevel: lg.level || 'verbose',
		enableLogs,
		timingStats: lg.timing_stats !== undefined ? lg.timing_stats : false,
		dailyLogLimit: lg.max_age || 7,
		webhookLogLimit: lg.webhook_log_limit || 12,
	}

	// ---- tuning ----
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
