process.title = 'poracle-alerter'
// eslint-disable-next-line no-underscore-dangle
require('events').EventEmitter.prototype._maxListeners = 100
const { writeHeapSnapshot } = require('v8')

const fs = require('fs')
const util = require('util')
const fastify = require('fastify')({
	bodyLimit: 52428800,
	routerOptions: { maxParamLength: 256 },
})
const { Telegraf } = require('telegraf')
const path = require('path')
const chokidar = require('chokidar')
const telegramCommandParser = require('./lib/telegram/middleware/commandParser')
const telegramController = require('./lib/telegram/middleware/controller')
const DiscordReconciliation = require('./lib/discord/discordReconciliation')
const TelegramReconciliation = require('./lib/telegram/telegramReconciliation')
const scannerFactory = require('./lib/scanner/scannerFactory')

const { Config } = require('./lib/configFetcher')
const GameData = require('./lib/GameData')

const {
	config, knex, scannerKnex, geofence, translatorFactory,
} = Config()

const PoracleInfo = {}

const readDir = util.promisify(fs.readdir)

const telegraf = new Telegraf(config.telegram.token)// , { channelMode: true })
const telegrafChannel = config.telegram.channelToken ? new Telegraf(config.telegram.channelToken)/* , { channelMode: true }) */ : null

const scannerQuery = scannerFactory.createScanner(scannerKnex, config.database.scannerType)

const DiscordCommando = require('./lib/discord/commando')

const TelegramWorker = require('./lib/telegram/Telegram')

const logs = require('./lib/logger')

const { log } = logs
const re = require('./util/regex')(translatorFactory)

const Query = require('./controllers/query')

const query = new Query(logs.controller, knex, config, geofence)

logs.setWorkerId('MAIN')
fastify.decorate('logger', logs.log)
fastify.decorate('config', config)
fastify.decorate('knex', knex)
fastify.decorate('GameData', GameData)
fastify.decorate('query', query)
fastify.decorate('scannerQuery', scannerQuery)
fastify.decorate('geofence', geofence)
fastify.decorate('translatorFactory', translatorFactory)

const discordCommando = config.discord.enabled ? new DiscordCommando(config.discord.token[0], query, scannerQuery, config, logs, GameData, PoracleInfo, geofence, translatorFactory) : null
let telegram
let telegramChannel

if (config.telegram.enabled) {
	telegram = new TelegramWorker('1', config, logs, GameData, PoracleInfo, geofence, telegramController, query, scannerQuery, telegraf, translatorFactory, telegramCommandParser, re)

	if (telegrafChannel) {
		telegramChannel = new TelegramWorker('2', config, logs, GameData, PoracleInfo, geofence, telegramController, query, scannerQuery, telegrafChannel, translatorFactory, telegramCommandParser, re)
	}
}

let telegramReconciliation

async function syncTelegramMembership() {
	try {
		if (!telegramReconciliation) {
			telegramReconciliation = new TelegramReconciliation(telegraf, log, config, query)
		}
		log.verbose('Verification of Telegram group membership for Poracle users starting...')

		if (config.reconciliation.telegram.updateUserNames || config.reconciliation.telegram.removeInvalidUsers) {
			await telegramReconciliation.syncTelegramUsers(
				config.reconciliation.discord.updateUserNames,
				config.reconciliation.discord.removeInvalidUsers,
			)
		}
		if (config.areaSecurity.enabled) {
			await telegramReconciliation.updateTelegramChannels()
		}
	} catch (err) {
		log.error('Verification of Poracle user\'s roles failed with', err)
	}
	setTimeout(syncTelegramMembership, config.telegram.checkRoleInterval * 3600000)
}

let discordReconciliation

async function syncDiscordRole() {
	try {
		if (!discordReconciliation) {
			if (!discordCommando || !discordCommando.client || !discordCommando.client.isReady()) {
				// try again in 30 seconds
				setTimeout(syncDiscordRole, 30000)
				return
			}
			discordReconciliation = new DiscordReconciliation(discordCommando.client, log, config, query)
		}
		// "updateChannelNames": true,
		// 	"updateChannelNotes": true,
		// 	"unregisterMissingChannels": true
		if (config.reconciliation.discord.updateChannelNames || config.reconciliation.discord.updateChannelNotes
			|| config.reconciliation.discord.unregisterMissingChannels) {
			await discordReconciliation.syncDiscordChannels(
				config.reconciliation.discord.updateChannelNames,
				config.reconciliation.discord.updateChannelNotes,
				config.reconciliation.discord.unregisterMissingChannels,
			)
		}
		// "updateUserNames": true,
		// "removeInvalidUsers": true,
		// "registerNewUsers": true,
		if (config.reconciliation.discord.updateUserNames || config.reconciliation.discord.removeInvalidUsers || config.reconciliation.discord.registerNewUsers) {
			await discordReconciliation.syncDiscordRole(
				config.reconciliation.discord.registerNewUsers,
				config.reconciliation.discord.updateUserNames,
				config.reconciliation.discord.removeInvalidUsers,
			)
		}
	} catch (err) {
		log.error('Verification of Poracle user\'s roles failed with', err)
	}
	setTimeout(syncDiscordRole, config.discord.checkRoleInterval * 3600000)
}

let shuttingDown = false

function handleShutdown() {
	if (shuttingDown) return
	shuttingDown = true

	log.info('Poracle shutdown - complete')
	process.exit()
}

function notifyProcessorReload() {
	if (config.processor.url) {
		const axios = require('axios')
		axios.post(`${config.processor.url}/api/reload`, null, { headers: config.processor.headers }).catch((err) => {
			log.error(`Failed to notify processor of reload: ${err.message}`)
		})
	}
}

function processMessages(msgs) {
	if (!config.processor.url) {
		log.warn('Cannot deliver command messages: processor URL not configured')
		return
	}
	const axios = require('axios')
	axios.post(`${config.processor.url}/api/deliverMessages`, msgs, { headers: config.processor.headers }).catch((err) => {
		log.error(`Failed to deliver command messages to processor: ${err.message}`)
	})
}

process.on('SIGUSR2', () => {
	writeHeapSnapshot()
})

async function currentStatus() {
	const mainMem = process.memoryUsage()
	const mainMemMb = Math.round(mainMem.heapUsed / 1048576)
	const mainRssMb = Math.round(mainMem.rss / 1048576)

	const infoMessage = `[Main] heap:${mainMemMb}MB rss:${mainRssMb}MB`
	log.info(infoMessage)

	PoracleInfo.status = {
		queueInfo: infoMessage,
		mainMemoryMb: mainMemMb,
		mainRssMb,
	}
}

async function run() {
	process.on('SIGINT', handleShutdown)
	process.on('SIGTERM', handleShutdown)

	let watchGeofence = Array.isArray(config.geofence.path)
		? config.geofence.path
		: [config.geofence.path]
	watchGeofence = watchGeofence.map((x) => (x.startsWith('http')
		? path.join(__dirname, '../.cache', `${x.replace(/\//g, '__')}.json`)
		: path.join(__dirname, `../${x}`)))

	chokidar.watch(watchGeofence, {
		awaitWriteFinish: true,
	}).on('change', () => {
		log.info('Change in geofence detected, triggering reload')
		try {
			const newGeofence = require('./lib/geofenceLoader').readAllGeofenceFiles(config)

			// Update main geofence reference
			geofence.rbush = newGeofence.rbush
			geofence.geofence = newGeofence.geofence
		} catch (err) {
			log.error('Error reloading geofence', err)
		}
	})

	if (config.discord.enabled) {
		try {
			log.info('Starting discord commando')

			await discordCommando.start()

			fastify.decorate('discordClient', discordCommando.client)
		} catch (err) {
			log.error('Error starting discord commando', err)
		}

		if (config.discord.checkRole && config.discord.checkRoleInterval && config.discord.guilds) {
			setTimeout(syncDiscordRole, 10000)
		}

		discordCommando.on('sendMessages', (res) => {
			processMessages(res)
		})

		discordCommando.on('refreshAlertCache', () => {
			fastify.triggerReloadAlerts()
		})
	}

	if (config.telegram.enabled) {
		try {
			log.info('Starting telegram bot')

			await telegram.start()
			if (telegramChannel) await telegramChannel.start()
		} catch (err) {
			log.error('Error starting telegram bot', err)
		}

		if (config.telegram.checkRole && config.telegram.checkRoleInterval) {
			setTimeout(syncTelegramMembership, 30000)
		}

		telegram.on('sendMessages', (res) => {
			processMessages(res)
		})

		telegram.on('refreshAlertCache', () => {
			fastify.triggerReloadAlerts()
		})
	}

	fastify.decorate('triggerReloadAlerts', notifyProcessorReload)

	const routeFiles = await readDir(`${__dirname}/routes/`)
	const routes = routeFiles.map((fileName) => `${__dirname}/routes/${fileName}`)

	routes.forEach((route) => fastify.register(require(route)))
	await fastify.listen({
		port: config.server.port,
		host: config.server.host,
	})
	log.info(`Service started on ${fastify.server.address().address}:${fastify.server.address().port}`)
}

function startPoracle() {
	run()
	setInterval(currentStatus, 60000)
}

const NODE_MAJOR_VERSION = process.versions.node.split('.')[0]
if (NODE_MAJOR_VERSION < 16) {
	log.warn('PoracleNG requires Node 16 - please upgrade')
	process.exit(1)
}

// Database migrations are handled by the Go processor on startup.
startPoracle()
