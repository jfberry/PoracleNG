const { parentPort, workerData, isMainThread } = require('worker_threads')
const { writeHeapSnapshot } = require('v8')
// eslint-disable-next-line no-underscore-dangle
require('events').EventEmitter.prototype._maxListeners = 100
const NodeCache = require('node-cache')
const PogoEventParser = require('./lib/pogoEventParser')
const ShinyPossible = require('./lib/shinyLoader')
const logs = require('./lib/logger')
const GameData = require('./lib/GameData')

const { log } = logs

const { Config } = require('./lib/configFetcher')
const mustache = require('./lib/handlebars')()

const { workerId } = workerData
logs.setWorkerId(`MATCHED-${workerId}`)

const {
	config, knex, dts, geofence, translatorFactory,
} = Config(false)

const PromiseQueue = require('./lib/PromiseQueue')

const MonsterController = require('./controllers/monster')
const RaidController = require('./controllers/raid')
const WeatherController = require('./controllers/weather')
const PokestopController = require('./controllers/pokestop')
const PokestopLureController = require('./controllers/pokestop_lure')
const QuestController = require('./controllers/quest')
const GymController = require('./controllers/gym')
const NestController = require('./controllers/nest')
const FortUpdateController = require('./controllers/fortupdate')
const CachingGeocoder = require('./lib/cachingGeocoder')

const rateLimitedUserCache = new NodeCache({ stdTTL: config.alertLimits.timingPeriod })

const pogoEventParser = new PogoEventParser(log)
const shinyPossible = new ShinyPossible(log)
const cachingGeocoder = new CachingGeocoder(config, log, mustache, `geoCache-matched-${workerId}`)

const eventParsers = {
	shinyPossible,
	pogoEvents: pogoEventParser,
}

// Create a stub weatherData/statsData since the processor provides this data
const stubWeatherData = {
	getWeatherCellId: () => '',
	checkWeatherOnMonster: () => {},
	getCurrentWeatherInCell: () => 0,
	getWeatherTimes: () => ({ nextHourTimestamp: 0 }),
	getWeatherForecast: async () => ({ current: 0, next: 0 }),
	receiveWeatherBroadcast: () => {},
}

const stubStatsData = {
	rarityGroups: {},
	shinyData: {},
	receiveStatsBroadcast: () => {},
}

const monsterController = new MonsterController(logs.controller, knex, cachingGeocoder, null, config, dts, geofence, GameData, rateLimitedUserCache, translatorFactory, mustache, stubWeatherData, stubStatsData, eventParsers)
const raidController = new RaidController(logs.controller, knex, cachingGeocoder, null, config, dts, geofence, GameData, rateLimitedUserCache, translatorFactory, mustache, stubWeatherData, stubStatsData, eventParsers)
const weatherController = new WeatherController(logs.controller, knex, cachingGeocoder, null, config, dts, geofence, GameData, rateLimitedUserCache, translatorFactory, mustache, null, null, null)
const pokestopController = new PokestopController(logs.controller, knex, cachingGeocoder, null, config, dts, geofence, GameData, rateLimitedUserCache, translatorFactory, mustache, stubWeatherData, stubStatsData, eventParsers)
const pokestopLureController = new PokestopLureController(logs.controller, knex, cachingGeocoder, null, config, dts, geofence, GameData, rateLimitedUserCache, translatorFactory, mustache, stubWeatherData, stubStatsData, eventParsers)
const questController = new QuestController(logs.controller, knex, cachingGeocoder, null, config, dts, geofence, GameData, rateLimitedUserCache, translatorFactory, mustache, stubWeatherData, stubStatsData, eventParsers)
const gymController = new GymController(logs.controller, knex, cachingGeocoder, null, config, dts, geofence, GameData, rateLimitedUserCache, translatorFactory, mustache, stubWeatherData, stubStatsData, eventParsers)
const nestController = new NestController(logs.controller, knex, cachingGeocoder, null, config, dts, geofence, GameData, rateLimitedUserCache, translatorFactory, mustache, stubWeatherData, stubStatsData, eventParsers)
const fortUpdateController = new FortUpdateController(logs.controller, knex, cachingGeocoder, null, config, dts, geofence, GameData, rateLimitedUserCache, translatorFactory, mustache, stubWeatherData, stubStatsData, eventParsers)

const hookQueue = []
let queuePort
let commandPort

async function processOne(payload) {
	let queueAddition = []

	try {
		if ((Math.random() * 1000) > 995) log.verbose(`Worker MATCHED-${workerId}: MatchedQueue is currently ${hookQueue.length}`)

		switch (payload.type) {
			case 'pokemon': {
				const result = await monsterController.handleMatched(payload.message, payload.matched_users, payload.matched_areas)
				if (result) {
					queueAddition = result
				} else {
					log.error(`Worker MATCHED-${workerId}: Missing result from pokemon matched processor`, { data: payload.message })
				}
				break
			}
			case 'raid':
			case 'egg': {
				const result = await raidController.handleMatched(payload.message, payload.matched_users, payload.matched_areas, payload.type)
				if (result) {
					queueAddition = result
				} else {
					log.error(`Worker MATCHED-${workerId}: Missing result from ${payload.type} matched processor`, { data: payload.message })
				}
				break
			}
			case 'weather_change': {
				const result = await weatherController.handleMatched(payload.message, payload.matched_users, payload.matched_areas)
				if (result) {
					queueAddition = result
				} else {
					log.error(`Worker MATCHED-${workerId}: Missing result from weather_change matched processor`, { data: payload.message })
				}
				break
			}
			case 'invasion': {
				const result = await pokestopController.handleMatched(payload.message, payload.matched_users, payload.matched_areas)
				if (result) {
					queueAddition = result
				} else {
					log.error(`Worker MATCHED-${workerId}: Missing result from invasion matched processor`, { data: payload.message })
				}
				break
			}
			case 'lure': {
				const result = await pokestopLureController.handleMatched(payload.message, payload.matched_users, payload.matched_areas)
				if (result) {
					queueAddition = result
				} else {
					log.error(`Worker MATCHED-${workerId}: Missing result from lure matched processor`, { data: payload.message })
				}
				break
			}
			case 'quest': {
				const result = await questController.handleMatched(payload.message, payload.matched_users, payload.matched_areas)
				if (result) {
					queueAddition = result
				} else {
					log.error(`Worker MATCHED-${workerId}: Missing result from quest matched processor`, { data: payload.message })
				}
				break
			}
			case 'gym': {
				const result = await gymController.handleMatched(payload.message, payload.matched_users, payload.matched_areas)
				if (result) {
					queueAddition = result
				} else {
					log.error(`Worker MATCHED-${workerId}: Missing result from gym matched processor`, { data: payload.message })
				}
				break
			}
			case 'nest': {
				const result = await nestController.handleMatched(payload.message, payload.matched_users, payload.matched_areas)
				if (result) {
					queueAddition = result
				} else {
					log.error(`Worker MATCHED-${workerId}: Missing result from nest matched processor`, { data: payload.message })
				}
				break
			}
			case 'fort_update': {
				const result = await fortUpdateController.handleMatched(payload.message, payload.matched_users, payload.matched_areas)
				if (result) {
					queueAddition = result
				} else {
					log.error(`Worker MATCHED-${workerId}: Missing result from fort_update matched processor`, { data: payload.message })
				}
				break
			}
			default:
				log.debug(`Worker MATCHED-${workerId}: Unhandled matched type ${payload.type}`)
		}

		if (queueAddition && queueAddition.length) {
			await queuePort.postMessage({
				queue: queueAddition,
			})
		}
	} catch (err) {
		log.error(`Worker MATCHED-${workerId}: Matched processor error`, err)
	}
}

const alarmProcessor = new PromiseQueue(hookQueue, config.tuning.concurrentWebhookProcessorsPerWorker)

function receiveQueue(msg) {
	try {
		hookQueue.push(msg)
		alarmProcessor.run(processOne, async (err) => {
			// eslint-disable-next-line no-console
			console.error(err)
			log.error(`Worker MATCHED-${workerId}: alarmProcessor exception`, err)
		})
	} catch (err) {
		log.error(`Worker MATCHED-${workerId}: receiveCommand failed to add new queue entry`, err)
	}
}

function updateBadGuys(badguys) {
	rateLimitedUserCache.flushAll()
	for (const guy of badguys) {
		rateLimitedUserCache.set(guy.key, guy.ttlTimeout, Math.max((guy.ttlTimeout - Date.now()) / 1000, 1))
	}
}

function reloadDts() {
	try {
		const newDts = require('./lib/dtsloader').readDtsFiles()
		monsterController.setDts(newDts)
		raidController.setDts(newDts)
		weatherController.setDts(newDts)
		pokestopController.setDts(newDts)
		pokestopLureController.setDts(newDts)
		questController.setDts(newDts)
		gymController.setDts(newDts)
		nestController.setDts(newDts)
		fortUpdateController.setDts(newDts)
		log.info('DTS reloaded in matched worker')
	} catch (err) {
		log.error('Error reloading dts in matched worker', err)
	}
}

function reloadGeofence() {
	try {
		const newGeofence = require('./lib/geofenceLoader').readAllGeofenceFiles(config)
		monsterController.setGeofence(newGeofence)
		raidController.setGeofence(newGeofence)
		weatherController.setGeofence(newGeofence)
		pokestopController.setGeofence(newGeofence)
		pokestopLureController.setGeofence(newGeofence)
		questController.setGeofence(newGeofence)
		gymController.setGeofence(newGeofence)
		nestController.setGeofence(newGeofence)
		fortUpdateController.setGeofence(newGeofence)
		log.info('Geofence reloaded in matched worker')
	} catch (err) {
		log.error('Error reloading geofence in matched worker', err)
	}
}

function receiveCommand(cmd) {
	try {
		log.debug(`Worker MATCHED-${workerId}: receiveCommand ${cmd.type}`)

		if (cmd.type === 'heapdump') {
			writeHeapSnapshot()
			return
		}

		if (cmd.type === 'badguys') {
			updateBadGuys(cmd.badguys)
		}
		if (cmd.type === 'eventBroadcast') {
			pogoEventParser.loadEvents(cmd.data)
		}
		if (cmd.type === 'shinyBroadcast') {
			shinyPossible.loadMap(cmd.data)
		}
		if (cmd.type === 'reloadDts') {
			reloadDts()
		}
		if (cmd.type === 'reloadGeofence') {
			reloadGeofence()
		}
	} catch (err) {
		log.error(`Worker MATCHED-${workerId}: receiveCommand failed to process command`, err)
	}
}

if (!isMainThread) {
	process.on('unhandledRejection', (reason) => {
		// eslint-disable-next-line no-console
		console.error(`Worker MATCHED-${workerId} Unhandled Rejection at: ${reason.stack || reason}`)
		log.error(`Unhandled Rejection at: ${reason.stack || reason}`)
	})

	process.on('uncaughtException', (err) => {
		// eslint-disable-next-line no-console
		console.error(`Worker MATCHED-${workerId} Uncaught Exception: ${err.stack || err}`)
		log.error(err)
	})

	parentPort.on('message', (msg) => {
		if (msg.type === 'queuePort') {
			queuePort = msg.queuePort
			commandPort = msg.commandPort

			msg.commandPort.on('message', receiveCommand)
			msg.queuePort.on('message', receiveQueue)
		}
	})

	monsterController.on('postMessage', (jobs) => queuePort.postMessage({ queue: jobs }))
	raidController.on('postMessage', (jobs) => queuePort.postMessage({ queue: jobs }))
	weatherController.on('postMessage', (jobs) => queuePort.postMessage({ queue: jobs }))
	pokestopController.on('postMessage', (jobs) => queuePort.postMessage({ queue: jobs }))
	pokestopLureController.on('postMessage', (jobs) => queuePort.postMessage({ queue: jobs }))
	questController.on('postMessage', (jobs) => queuePort.postMessage({ queue: jobs }))
	gymController.on('postMessage', (jobs) => queuePort.postMessage({ queue: jobs }))
	nestController.on('postMessage', (jobs) => queuePort.postMessage({ queue: jobs }))
	fortUpdateController.on('postMessage', (jobs) => queuePort.postMessage({ queue: jobs }))
}
