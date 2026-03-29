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

const DiscordWorker = require('./lib/discord/discordWorker')
const DiscordWebhookWorker = require('./lib/discord/discordWebhookWorker')
const DiscordCommando = require('./lib/discord/commando')

const TelegramWorker = require('./lib/telegram/Telegram')

const logs = require('./lib/logger')
const metrics = require('./lib/metrics')

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
fastify.decorate('discordQueue', [])
fastify.decorate('telegramQueue', [])

const discordCommando = config.discord.enabled ? new DiscordCommando(config.discord.token[0], query, scannerQuery, config, logs, GameData, PoracleInfo, geofence, translatorFactory) : null
const discordWorkers = []
let discordWebhookWorker
let telegram
let telegramChannel

if (config.discord.enabled) {
	for (let key = 0; key < config.discord.token.length; key++) {
		if (config.discord.token[key]) {
			discordWorkers.push(new DiscordWorker(config.discord.token[key], key + 1, config, logs, true, (key
				? { status: config.discord.workerStatus || 'invisible', activity: config.discord.workerActivity ?? 'PoracleHelper' }
				: { status: 'available', activity: config.discord.activity ?? 'PoracleNG' }), query))
		}
	}
	fastify.decorate('discordWorker', discordWorkers[0])
	discordWebhookWorker = new DiscordWebhookWorker(config, logs, true, query)
}

if (config.telegram.enabled) {
	telegram = new TelegramWorker('1', config, logs, GameData, PoracleInfo, geofence, telegramController, query, scannerQuery, telegraf, translatorFactory, telegramCommandParser, re, true)

	if (telegrafChannel) {
		telegramChannel = new TelegramWorker('2', config, logs, GameData, PoracleInfo, geofence, telegramController, query, scannerQuery, telegrafChannel, translatorFactory, telegramCommandParser, re, true)
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
			const worker = discordWorkers[0]
			if (!worker || worker.busy) {
				// try again in 30 seconds
				setTimeout(syncDiscordRole, 30000)
				return
			}
			discordReconciliation = new DiscordReconciliation(worker.client, log, config, query)
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

	log.info('Poracle shutdown - saving cache')

	const workerSaves = []
	for (const worker of discordWorkers) {
		workerSaves.push(worker.saveTimeouts())
	}
	if (telegram) workerSaves.push(telegram.saveTimeouts())
	if (telegramChannel) workerSaves.push(telegramChannel.saveTimeouts())
	if (discordWebhookWorker) workerSaves.push(discordWebhookWorker.saveTimeouts())

	Promise.all(workerSaves)
		.then(() => log.info('Poracle shutdown - complete'))
		.catch((err) => log.error(`Poracle shutdown - Error saving files ${err}`))
		.finally(() => process.exit())
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
	for (const msg of msgs) {
		if (['discord:user', 'discord:channel', 'webhook'].includes(msg.type)) fastify.discordQueue.push(msg)
		if (['telegram:user', 'telegram:channel', 'telegram:group'].includes(msg.type)) fastify.telegramQueue.push(msg)
	}
}

process.on('SIGUSR2', () => {
	writeHeapSnapshot()
})

async function currentStatus() {
	let discordQueueLength = 0

	// eslint-disable-next-line no-sequences
	const queueCount = (queue) => queue.map((x) => x.target).reduce((r, c) => (r[c] = (r[c] || 0) + 1, r), {})

	const queueSummary = {}

	for (const w of discordWorkers) {
		discordQueueLength += w.discordQueue.length
		Object.assign(queueSummary, queueCount(w.discordQueue))
	}

	const telegramQueueLength = (telegram ? telegram.telegramQueue.length : 0)
		+ (telegramChannel ? telegramChannel.telegramQueue.length : 0)

	const webhookQueueLength = discordWebhookWorker ? discordWebhookWorker.webhookQueue.length : 0
	Object.assign(
		queueSummary,
		telegram ? queueCount(telegram.telegramQueue) : {},
		telegramChannel ? queueCount(telegramChannel.telegramQueue) : {},
		discordWebhookWorker ? queueCount(discordWebhookWorker.webhookQueue) : {},
	)

	const mainMem = process.memoryUsage()
	const mainMemMb = Math.round(mainMem.heapUsed / 1048576)
	const mainRssMb = Math.round(mainMem.rss / 1048576)

	// Update prometheus gauges
	metrics.discordQueueDepth.set(discordQueueLength)
	metrics.discordWebhookQueueDepth.set(webhookQueueLength)
	metrics.telegramQueueDepth.set(telegramQueueLength)

	const infoMessage = `[Main] Queues: Discord: ${discordQueueLength} + ${webhookQueueLength} | Telegram: ${telegramQueueLength} | heap:${mainMemMb}MB rss:${mainRssMb}MB`
	log.info(infoMessage)

	PoracleInfo.status = {
		queueInfo: infoMessage,
		queueSummary,
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
			log.info('Starting discord workers')

			await discordCommando.start()
			for (const discordWorker of discordWorkers) {
				await discordWorker.start()
			}
			await discordWebhookWorker.start()

			fastify.decorate('discordClient', discordWorkers[0].client)
		} catch (err) {
			log.error('Error starting discord workers', err)
		}

		setInterval(() => {
			if (!fastify.discordQueue.length) {
				return
			}

			// Dequeue onto individual queues as fast as possible
			while (fastify.discordQueue.length) {
				const { target, type } = fastify.discordQueue[0]
				let discordWorker
				if (type === 'webhook') {
					discordWorker = discordWebhookWorker
				} else {
					// see if target has dedicated worker
					discordWorker = discordWorkers.find((workerr) => workerr.users.includes(target))
					if (!discordWorker) {
						let busyestWorkerHumanCount = Number.POSITIVE_INFINITY
						let laziestWorkerId
						Object.keys(discordWorkers).map((i) => {
							if (discordWorkers[i].userCount < busyestWorkerHumanCount) {
								busyestWorkerHumanCount = discordWorkers[i].userCount
								laziestWorkerId = i
							}
						})
						busyestWorkerHumanCount = Number.POSITIVE_INFINITY
						discordWorker = discordWorkers[laziestWorkerId]
						discordWorker.addUser(target)
					}
				}

				discordWorker.work(fastify.discordQueue.shift())
			}
		}, 100)

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
			log.info('Starting telegram workers')

			await telegram.start()
			if (telegramChannel) await telegramChannel.start()
		} catch (err) {
			log.error('Error starting discord workers', err)
		}
		setInterval(() => {
			if (!fastify.telegramQueue.length) {
				return
			}

			while (fastify.telegramQueue.length) {
				let telegramWorker = telegram
				if (telegramChannel && ['telegram:channel', 'telegram:group'].includes(fastify.telegramQueue[0].type)) {
					telegramWorker = telegramChannel
				}

				telegramWorker.work(fastify.telegramQueue.shift())
			}
		}, 100)

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
