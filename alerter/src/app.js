process.title = 'PoracleJS'
require('./lib/configFileCreator')()
// eslint-disable-next-line no-underscore-dangle
require('events').EventEmitter.prototype._maxListeners = 100
const { writeHeapSnapshot } = require('v8')

const fs = require('fs')
const util = require('util')
const { Worker, MessageChannel } = require('worker_threads')
const pcache = require('flat-cache')
const fastify = require('fastify')({
	bodyLimit: 52428800,
	maxParamLength: 256,
})
const { Telegraf } = require('telegraf')
const path = require('path')
const chokidar = require('chokidar')
const moment = require('moment-timezone')
const geoTz = require('geo-tz')
const schedule = require('node-schedule')
const telegramCommandParser = require('./lib/telegram/middleware/commandParser')
const telegramController = require('./lib/telegram/middleware/controller')
const DiscordReconciliation = require('./lib/discord/discordReconciliation')
const TelegramReconciliation = require('./lib/telegram/telegramReconciliation')
const PogoEventParser = require('./lib/pogoEventParser')
const scannerFactory = require('./lib/scanner/scannerFactory')
const ShinyPossible = require('./lib/shinyLoader')

const { Config } = require('./lib/configFetcher')
const GameData = require('./lib/GameData')

const {
	config, knex, scannerKnex, dts, geofence, translatorFactory,
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

const { log } = logs
const re = require('./util/regex')(translatorFactory)

const Query = require('./controllers/query')

const query = new Query(logs.controller, knex, config, geofence)
const pogoEventParser = new PogoEventParser(logs.log)
const shinyPossible = new ShinyPossible(logs.log)

logs.setWorkerId('MAIN')
fastify.decorate('logger', logs.log)
fastify.decorate('controllerLog', logs.controller)
fastify.decorate('matchedWebhooks', logs.matchedWebhooks)
fastify.decorate('config', config)
fastify.decorate('knex', knex)
fastify.decorate('GameData', GameData)
fastify.decorate('query', query)
fastify.decorate('scannerQuery', scannerQuery)
fastify.decorate('dts', dts)
fastify.decorate('geofence', geofence)
fastify.decorate('translatorFactory', translatorFactory)
fastify.decorate('discordQueue', [])
fastify.decorate('telegramQueue', [])
fastify.decorate('matchedQueue', [])

const discordCommando = config.discord.enabled ? new DiscordCommando(config.discord.token[0], query, scannerQuery, config, logs, GameData, PoracleInfo, dts, geofence, translatorFactory) : null
const discordWorkers = []
let discordWebhookWorker
let telegram
let telegramChannel

if (config.discord.enabled) {
	for (let key = 0; key < config.discord.token.length; key++) {
		if (config.discord.token[key]) {
			discordWorkers.push(new DiscordWorker(config.discord.token[key], key + 1, config, logs, true, (key
				? { status: config.discord.workerStatus || 'invisible', activity: config.discord.workerActivity ?? 'PoracleHelper' }
				: { status: 'available', activity: config.discord.activity ?? 'PoracleJS' }), query))
		}
	}
	fastify.decorate('discordWorker', discordWorkers[0])
	discordWebhookWorker = new DiscordWebhookWorker(config, logs, true, query)
}

if (config.telegram.enabled) {
	telegram = new TelegramWorker('1', config, logs, GameData, PoracleInfo, dts, geofence, telegramController, query, scannerQuery, telegraf, translatorFactory, telegramCommandParser, re, true)

	if (telegrafChannel) {
		telegramChannel = new TelegramWorker('2', config, logs, GameData, PoracleInfo, dts, geofence, telegramController, query, scannerQuery, telegrafChannel, translatorFactory, telegramCommandParser, re, true)
	}
}

let telegramReconciliation

async function syncTelegramMembership() {
	try {
		if (!telegramReconciliation) {
			telegramReconciliation = new TelegramReconciliation(telegraf, log, config, query, dts)
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
			discordReconciliation = new DiscordReconciliation(worker.client, log, config, query, dts)
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

function handleShutdown() {
	log.info('Poracle shutdown - starting save of cache')
	const workerSaves = []
	for (const worker of discordWorkers) {
		workerSaves.push(worker.saveTimeouts())
	}
	if (telegram) workerSaves.push(telegram.saveTimeouts())
	if (telegramChannel) workerSaves.push(telegramChannel.saveTimeouts())
	if (discordWebhookWorker) workerSaves.push(discordWebhookWorker.saveTimeouts())

	Promise.all(workerSaves)
		.then(() => {
			log.info('Poracle shutdown - complete')
			process.exit()
		}).catch((err) => {
			log.error(`Poracle shutdown - Error saving files ${err}`)
			process.exit()
		})
}

async function processPogoEvents() {
	let file
	log.info('PogoEvents: Fetching new event file')

	try {
		file = await pogoEventParser.download()
	} catch (err) {
		log.error('PogoEvents: Cannot download pogo event file', err)
		setTimeout(processPogoEvents, 15 * 60 * 1000) // 15 mins
		return
	}

	sendCommandToWorkers({
		type: 'eventBroadcast',
		data: file,
	})

	setTimeout(processPogoEvents, 6 * 60 * 60 * 1000) // 6 hours
}

async function processPossibleShiny() {
	let file
	log.info('ShinyPossible: Fetching new shiny file')

	try {
		file = await shinyPossible.download()
	} catch (err) {
		log.error('ShinyPossible: Cannot shiny file', err)
		setTimeout(processPossibleShiny, 15 * 60 * 1000) // 15 mins
		return
	}

	sendCommandToWorkers({
		type: 'shinyBroadcast',
		data: file,
	})

	setTimeout(processPossibleShiny, 6 * 60 * 60 * 1000) // 6 hours
}

const UserRateChecker = require('./userRateLimit')

const rateChecker = new UserRateChecker(config)

async function processMessages(msgs) {
	let newRateLimits = false

	for (const msg of msgs) {
		const destinationId = msg.type === 'webhook' ? msg.name : msg.target
		const destinationType = msg.type
		const rate = rateChecker.validateMessage(destinationId, destinationType)

		let queueMessage
		let logMessage = null
		let shameMessage = null

		if (!msg.alwaysSend && !rate.passMessage) {
			if (rate.justBreached) {
				const userTranslator = translatorFactory.Translator(msg.language || config.general.locale)
				queueMessage = {
					...msg,
					message: { content: userTranslator.translateFormat('You have reached the limit of {0} messages over {1} seconds', rate.messageLimit, rate.messageTimeout) },
					emoji: [],
				}
				log.info(`${msg.logReference}: Stopping alerts (Rate limit) for ${msg.type} ${msg.target} ${msg.name} Time to release: ${rate.resetTime}`)

				if (config.alertLimits.maxLimitsBeforeStop) {
					const userCheck = rateChecker.userIsBanned(destinationId, destinationType)
					if (!userCheck.canContinue) {
						queueMessage = {
							...msg,
							message: {
								content: userTranslator.translateFormat(
									config.alertLimits.disableOnStop
										? 'You have breached the rate limit too many times in the last 24 hours. Your messages are now stopped, contact an administrator to resume'
										: 'You have breached the rate limit too many times in the last 24 hours. Your messages are now stopped, use {0}start to resume',
									['discord:user', 'discord:channel', 'webhook'].includes(msg.type) ? config.discord.prefix : '/',
								),
							},
							emoji: [],
						}

						log.info(`${msg.logReference}: Stopping alerts [until restart] (Rate limit) for ${msg.type} ${msg.target} ${msg.name}`)

						logMessage = `Stopped alerts (rate-limit exceeded too many times) for target ${destinationType} ${destinationId} ${msg.name} ${msg.type === 'discord:user' ? `<@${destinationId}>` : ''}`
						if (msg.type === 'discord:user') {
							shameMessage = userTranslator.translateFormat('<@{0}> has had their Poracle tracking disabled for exceeding the rate limit too many times!', destinationId)
						}

						try {
							if (config.alertLimits.disableOnStop) {
								// This acts like the admin de-registered the user rather than when losing a role so user does not get auto re-registered
								await query.updateQuery('humans', { admin_disable: 1, disabled_date: null }, { id: msg.target })
							} else {
								await query.updateQuery('humans', { enabled: 0 }, { id: msg.target })
							}
						} catch (err) {
							log.error('Failed to stop user messages', err)
						}
					}
				}

				newRateLimits = true
			} else {
				log.info(`${msg.logReference}: Intercepted and stopped message for user (Rate limit) for ${msg.type} ${msg.target} ${msg.name} Time to release: ${rate.resetTime}`)

				queueMessage = null
			}
		} else {
			queueMessage = msg
		}

		if (queueMessage) {
			if (['discord:user', 'discord:channel', 'webhook'].includes(queueMessage.type)) fastify.discordQueue.push(queueMessage)
			if (['telegram:user', 'telegram:channel', 'telegram:group'].includes(queueMessage.type)) fastify.telegramQueue.push(queueMessage)
			if (logMessage && config.discord.dmLogChannelID) {
				fastify.discordQueue.push({
					lat: 0,
					lon: 0,
					message: {
						content: logMessage,
					},
					target: config.discord.dmLogChannelID,
					type: 'discord:channel',
					name: 'Log channel',
					tth: { hours: 0, minutes: config.discord.dmLogChannelDeletionTime, seconds: 0 },
					clean: config.discord.dmLogChannelDeletionTime > 0,
					emoji: '',
					logReference: queueMessage.logReference,
					language: config.general.locale,
				})
			}
			if (shameMessage && config.alertLimits.shameChannel) {
				fastify.discordQueue.push({
					lat: 0,
					lon: 0,
					message: {
						content: shameMessage,
					},
					target: config.alertLimits.shameChannel,
					type: 'discord:channel',
					name: 'Shame channel',
					tth: { hours: 0, minutes: 0, seconds: 0 },
					clean: false,
					emoji: '',
					logReference: queueMessage.logReference,
					language: config.general.locale,
				})
			}
		}
	}

	if (newRateLimits) {
		// Publish new rate limits to matched workers
		const badguys = rateChecker.getBadBoys()

		sendCommandToWorkers({
			type: 'badguys',
			badguys,
		})
	}
}

// Matched workers - handle pre-matched payloads from the Go processor
const matchedWorkers = []
const matchedWorkerStatuses = {}

const matchedWorkerHeapMb = config.tuning.matchedWorkerMaxOldGenerationSizeMb || 512
const maxMatchedWorkers = config.tuning.matchedProcessingWorkers || config.tuning.webhookProcessingWorkers || 3

for (let w = 0; w < maxMatchedWorkers; w++) {
	let worker = new Worker(path.join(__dirname, './matchedWorker.js'), {
		workerData: { workerId: w + 1 },
		resourceLimits: {
			maxOldGenerationSizeMb: matchedWorkerHeapMb,
			maxYoungGenerationSizeMb: Math.max(64, Math.floor(matchedWorkerHeapMb / 4)),
		},
	})

	let queueChannel = new MessageChannel()
	let commandChannel = new MessageChannel()

	worker.postMessage({
		type: 'queuePort',
		queuePort: queueChannel.port2,
		commandPort: commandChannel.port2,
	}, [queueChannel.port2, commandChannel.port2])

	queueChannel.port1.on('message', (res) => {
		if (res.type === 'status') {
			matchedWorkerStatuses[res.workerId] = res
			return
		}
		if (res.queue) {
			processMessages(res.queue)
		}
	})

	matchedWorkers.push({
		worker,
		commandPort: commandChannel.port1,
		queuePort: queueChannel.port1,
	})
}

function sendCommandToWorkers(msg) {
	for (const mw of matchedWorkers) {
		mw.commandPort.postMessage(msg)
	}
}

process.on('SIGUSR2', () => {
	writeHeapSnapshot()
	for (const mw of matchedWorkers) {
		mw.commandPort.postMessage({ type: 'heapdump' })
	}
})

let currentMatchedWorkerNo = 0

const matchedWorkerMaxQueue = config.tuning.matchedWorkerMaxQueueSize || 5000

function getTotalWorkerQueueDepth() {
	let total = 0
	for (const status of Object.values(matchedWorkerStatuses)) {
		total += (status.queueDepth || 0) + (status.activeJobs || 0)
	}
	return total
}

async function handleMatchedAlarms() {
	if (fastify.matchedQueue.length) {
		if ((Math.random() * 1000) > 995) fastify.logger.verbose(`Inbound MatchedQueue is currently ${fastify.matchedQueue.length}`)

		// Backpressure: if total worker queue depth is too high, wait before sending more
		const totalDepth = getTotalWorkerQueueDepth()
		if (totalDepth >= matchedWorkerMaxQueue) {
			if ((Math.random() * 1000) > 990) fastify.logger.warn(`Backpressure active: total worker queue depth ${totalDepth} >= limit ${matchedWorkerMaxQueue}, inbound queue ${fastify.matchedQueue.length}`)
			return
		}

		const payload = fastify.matchedQueue.shift()
		fastify.matchedWebhooks.info(`${payload.type} ${JSON.stringify(payload)}`)
		currentMatchedWorkerNo = (currentMatchedWorkerNo + 1) % matchedWorkers.length
		await matchedWorkers[currentMatchedWorkerNo].queuePort.postMessage(payload)
		setImmediate(handleMatchedAlarms)
	}
}

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

	const workerStatusList = Object.values(matchedWorkerStatuses)
	let workerInfo = ''
	if (workerStatusList.length) {
		const parts = workerStatusList.map((s) => `W${s.workerId}:${s.heapUsedMb}/${s.heapTotalMb}MB q:${s.queueDepth} a:${s.activeJobs}`)
		workerInfo = ` | Workers [${parts.join(', ')}]`
	}

	const totalWorkerDepth = getTotalWorkerQueueDepth()
	const infoMessage = `[Main] Queues: Matched inbound:${fastify.matchedQueue.length} workers:${totalWorkerDepth} | Discord: ${discordQueueLength} + ${webhookQueueLength} | Telegram: ${telegramQueueLength} | Main heap:${mainMemMb}MB rss:${mainRssMb}MB${workerInfo}`
	log.info(infoMessage)

	PoracleInfo.status = {
		queueInfo: infoMessage,
		queueSummary,
		mainMemoryMb: mainMemMb,
		mainRssMb,
		matchedWorkerStatuses,
	}
}

schedule.scheduleJob({ minute: [0, 10, 20, 30, 40, 50] }, async () => {			// Run every 10 minutes - note if this changes then check below also needs to change
	try {
		log.verbose('Profile Check: Checking for active profile changes')
		const humans = await query.selectAllQuery('humans', { enabled: 1, admin_disable: 0 })
		const profilesToCheck = await query.mysteryQuery('SELECT * FROM profiles WHERE LENGTH(active_hours)>5 ORDER BY id, profile_no')

		let lastId = null
		for (const profile of profilesToCheck) {
			const human = humans.find((x) => x.id === profile.id)

			// eslint-disable-next-line no-continue
			if (!human) continue

			let nowForHuman = moment()
			if (human.latitude) {
				nowForHuman = moment().tz(geoTz.find(human.latitude, human.longitude)[0].toString())
			}

			if (profile.id !== lastId) {
				const timings = JSON.parse(profile.active_hours)
				const nowHour = nowForHuman.hour()
				const nowMinutes = nowForHuman.minutes()
				const nowDow = nowForHuman.isoWeekday()
				const yesterdayDow = +nowDow === 1 ? 7 : nowDow - 1

				const active = timings.some((row) => {
					const rowHours = +row.hours
					const rowMins = +row.mins
					const rowDay = +row.day

					return (rowDay === nowDow && rowHours === nowHour && nowMinutes >= row.mins && (nowMinutes - rowMins) < 10) // within 10 minutes in same hour
						|| (nowMinutes < 10 && rowDay === nowDow && rowHours === nowHour - 1 && rowMins > 50) // first 10 minutes of new hour
						|| (nowHour === 0 && nowMinutes < 10 && rowDay === yesterdayDow && rowHours === 23 && rowMins > 50) // first 10 minutes of day
				})

				if (active) {
					if (human.current_profile_no !== profile.profile_no) {
						const userTranslator = translatorFactory.Translator(human.language || config.general.locale)

						const job = {
							type: human.type,
							target: human.id,
							name: human.name,
							ping: '',
							clean: false,
							message: { content: userTranslator.translateFormat('I have set your profile to: {0}', profile.name) },
							logReference: '',
							tth: { hours: 1, minutes: 0, seconds: 0 },
						}

						if (['discord:user', 'discord:channel', 'webhook'].includes(job.type)) fastify.discordQueue.push(job)
						if (['telegram:user', 'telegram:channel', 'telegram:group'].includes(job.type)) fastify.telegramQueue.push(job)

						log.info(`Profile Check: Setting ${profile.id} to profile ${profile.profile_no} - ${profile.name}`)

						lastId = profile.id
						await query.updateQuery(
							'humans',
							{
								current_profile_no: profile.profile_no,
								area: profile.area,
								latitude: profile.latitude,
								longitude: profile.longitude,
							},
							{ id: profile.id },
						)
					}
				}
			}
		}
	} catch (err) {
		log.error('Error setting profiles', err)
	}
})

async function run() {
	process.on('SIGINT', handleShutdown)
	process.on('SIGTERM', handleShutdown)

	setTimeout(processPogoEvents, 30000)
	setTimeout(processPossibleShiny, 30000)

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
			sendCommandToWorkers({
				type: 'reloadGeofence',
			})

			// This splice mechanism replaces array in place (relies on no caching)
			const newGeofence = require('./lib/geofenceLoader').readAllGeofenceFiles(config)
			geofence.rbush = newGeofence.rbush
			geofence.geofence = newGeofence.geofence
		} catch (err) {
			log.error('Error reloading dts', err)
		}
	})

	chokidar.watch([
		path.join(__dirname, '../config/dts.json'),
		path.join(__dirname, '../config/dts/'),
	], {
		awaitWriteFinish: true,
	}).on('change', () => {
		log.info('Change in DTS detected, triggering reload')
		try {
			sendCommandToWorkers({
				type: 'reloadDts',
			})

			// This splice mechanism replaces array in place (relies on no caching)
			const newDts = require('./lib/dtsloader').readDtsFiles()
			dts.splice(0, dts.length, ...newDts)
		} catch (err) {
			log.error('Error reloading dts', err)
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

		discordCommando.on('addMatchedQueue', (res) => {
			fastify.matchedQueue.push(res)
			handleMatchedAlarms()
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

		telegram.on('addMatchedQueue', (res) => {
			fastify.matchedQueue.push(res)
			handleMatchedAlarms()
		})

		telegram.on('refreshAlertCache', () => {
			fastify.triggerReloadAlerts()
		})
	}

	fastify.decorate('triggerReloadAlerts', () => {
		sendCommandToWorkers({
			type: 'refreshAlertCache',
		})

		// Notify the Go processor to reload its in-memory data
		if (config.processor.url) {
			const axios = require('axios')
			axios.post(`${config.processor.url}/api/reload`).catch((err) => {
				log.error(`Failed to notify processor of reload: ${err.message}`)
			})
		}
	})

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
	setInterval(handleMatchedAlarms, 100)
	setInterval(currentStatus, 60000)
}

const NODE_MAJOR_VERSION = process.versions.node.split('.')[0]
if (NODE_MAJOR_VERSION < 16) {
	log.warn('PoracleJS requires Node 16 - please upgrade')
	process.exit(1)
}

knex.migrate.latest({
	directory: path.join(__dirname, './lib/db/migrations'),
	tableName: 'migrations',
}).then(() => {
	startPoracle()
}).catch((err) => {
	// eslint-disable-next-line no-console
	console.error(err)

	log.error('Migration failed', err)

	if (process.argv.includes('--force')) {
		startPoracle()
	} else {
		// eslint-disable-next-line no-console
		console.error('Migration failed - exiting PoracleJS')

		process.exit(1)
	}
})
