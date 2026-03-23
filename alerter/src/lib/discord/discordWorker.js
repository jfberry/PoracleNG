const {
	Client, DiscordAPIError,
	GatewayIntentBits,
	Partials,
	Options,
} = require('discord.js')
const path = require('path')
const fsp = require('fs').promises
const NodeCache = require('node-cache')
const { performance } = require('perf_hooks')
const { getConfigDir } = require('../configResolver')

const CACHE_DIR = path.resolve(getConfigDir(), '.cache')
const FairPromiseQueue = require('../FairPromiseQueue')
const metrics = require('../metrics')

const noop = () => { }

function coerceEmbedColor(embed) {
	if (embed && typeof embed.color === 'string') {
		embed.color = parseInt(embed.color.replace(/^#/, ''), 16)
	}
}

class Worker {
	constructor(token, id, config, logs, rehydrateTimeouts, statusActivity, query) {
		this.id = id
		this.token = token
		this.config = config
		this.logs = logs
		this.busy = true
		this.users = []
		this.userCount = 0
		this.client = {}
		this.rehydrateTimeouts = rehydrateTimeouts
		this.discordMessageTimeouts = new NodeCache()
		this.consecutiveFails = new Map()
		this.discordQueue = []
		this.queueProcessor = new FairPromiseQueue(this.discordQueue, this.config.tuning.concurrentDiscordDestinationsPerBot, ((entry) => entry.target))
		this.status = statusActivity.status
		this.activity = statusActivity.activity
		this.query = query
	}

	async start() {
		await this.bounceWorker()
	}

	// eslint-disable-next-line class-methods-use-this,no-promise-executor-return
	async sleep(n) { return new Promise((resolve) => setTimeout(resolve, n)) }

	addUser(id) {
		this.users.push(id)
		this.userCount += 1
	}

	async setListeners() {
		this.client.on('error', (err) => {
			this.busy = true
			this.logs.discord.error(`Discord worker #${this.id} \n bouncing`, err)
			this.bounceWorker()
		})
		this.client.on('warn', (err) => {
			this.logs.discord.error(`Discord worker #${this.id} \n bouncing`, err)
		})
		this.client.on('clientReady', () => {
			this.logs.log.info(`discord worker #${this.id} ${this.client.user.username} ready for action`)
			if (this.rehydrateTimeouts) {
				this.loadTimeouts()
			}
			this.busy = false
		})
		this.client.rest.on('rateLimited', (info) => {
			const tag = (this.client && this.client.user) ? this.client.user.username : ''
			let channelId
			if (info.route) {
				const channelMatch = info.route.match(/\/channels\/(\d+)\//)
				if (channelMatch && channelMatch[1]) {
					const channel = this.client.channels.cache.get(channelMatch[1])
					if (channel) {
						channelId = channel.recipient && `DM:${channel.recipient.id}:${channel.recipient.username}`
							|| `${channel.id}:#${channel.name}`
					}
				}
			}
			this.logs.discord.warn(`#${this.id} Discord worker [${tag}] 429 rate limit hit - in timeout ${info.timeToReset ? info.timeToReset : 'Unknown timeout '} route ${info.route}${channelId ? ` (probably ${channelId})` : ''}`)
			metrics.discordRateLimits.inc({ source: 'bot' })
		})
	}

	async bounceWorker() {
		delete this.client

		this.client = new Client({
			intents: [
				GatewayIntentBits.Guilds,
				GatewayIntentBits.GuildMessages,
				GatewayIntentBits.GuildMembers,
				GatewayIntentBits.DirectMessages,
				GatewayIntentBits.GuildPresences,
				GatewayIntentBits.MessageContent,
			],
			partials: [Partials.Channel, Partials.Message],
			makeCache: Options.cacheWithLimits({
				MessageManager: 1,
				PresenceManager: 0,
			}),
		})

		try {
			await this.setListeners()
			await this.client.login(this.token)
			await this.client.user.setStatus(this.status)
			if (this.activity) await this.client.user.setActivity(this.activity)
		} catch (err) {
			if (err.code === 4014) {
				this.logs.log.error('Could not initialise discord', err)
				this.logs.log.error('Ensure that your discord bot Gateway intents for Presence, Server Members and Messages are on - see https://muckelba.github.io/poracleWiki/discordbot.html')
				process.exit(1)
			}
			this.logs.log.error(`Discord worker didn't bounce, \n ${err.message} \n trying again`)
			await this.sleep(2000)
			return this.bounceWorker()
		}
	}

	work(data) {
		this.discordQueue.push(data)
		if (!this.busy) {
			this.queueProcessor.run(
				async (work) => (this.sendAlert(work)),
				async (err) => {
					this.logs.log.error('Discord queueProcessor exception', err)
				},
			)
		}
	}

	async sendAlert(data) {
		if ((Math.random() * 100) > 95) this.logs.log.verbose(`#${this.id} DiscordQueue is currently ${this.discordQueue.length}`) // todo: per minute

		switch (data.type) {
			case 'discord:user': {
				await this.userAlert(data)
				break
			}
			case 'discord:channel': {
				await this.channelAlert(data)
				break
			}
			default:
		}
	}

	async userAlert(data) {
		let user = this.client.users.cache.get(data.target)
		let msgDeletionMs = 0
		if (data.clean) {
			const tth = data.tth || {
				days: 0, hours: 0, minutes: 0, seconds: 0,
			}
			msgDeletionMs = ((tth.days * 86400) + (tth.hours * 3600) + (tth.minutes * 60) + tth.seconds) * 1000
		}

		try {
			const logReference = data.logReference ? data.logReference : 'Unknown'

			this.logs.discord.info(`${logReference}: #${this.id} -> ${data.name} ${data.target} USER Sending discord message${data.clean ? ' (clean)' : ''}`)

			if (!user) {
				user = await this.client.users.fetch(data.target)
				await user.createDM()
			}

			coerceEmbedColor(data.message.embed)
			if (data.message.embed) {
				data.message.embeds = [data.message.embed]
				delete data.message.embed
			}
			if (data.message.embeds) {
				for (const embed of data.message.embeds) coerceEmbedColor(embed)
			}

			if (this.config.discord.uploadEmbedImages && data.message.embeds && data.message.embeds.length && data.message.embeds[0].image && data.message.embeds[0].image.url) {
				const { url } = data.message.embeds[0].image
				data.message.embeds[0].image.url = 'attachment://map.png'
				data.message.files = [{ attachment: url, name: 'map.png' }]
			}

			const startTime = performance.now()

			this.logs.discord.debug(`${logReference}: #${this.id} -> ${data.name} ${data.target} USER Sending discord message`, data.message)

			const msg = await user.send(/* data.message.content || '', */ data.message)
			const endTime = performance.now();
			(this.config.logger.timingStats ? this.logs.discord.verbose : this.logs.discord.debug)(`${logReference}: #${this.id} -> ${data.name} ${data.target} USER (${endTime - startTime} ms)`)
			metrics.discordDeliveryDuration.observe({ destination_type: 'user' }, (endTime - startTime) / 1000)
			metrics.messagesSent.inc({ destination_type: 'discord:user' })

			this.consecutiveFails.delete(data.target)

			if (data.clean) {
				setTimeout(async () => {
					try {
						await msg.delete()
					} catch (err) {
						this.logs.discord.error(`${data.logReference}: #${this.id} Failed to send clean Discord alert to ${data.name} ${data.target}`, err)
					}
				}, msgDeletionMs)
				this.discordMessageTimeouts.set(msg.id, { type: 'user', id: data.target }, Math.floor(msgDeletionMs / 1000) + 1)
			}
			return true
		} catch (err) {
			metrics.messagesFailed.inc({ destination_type: 'discord:user' })
			this.logs.discord.error(`${data.logReference}: #${this.id} Failed to send Discord alert to ${data.name}`, err, data)
			this.logs.discord.error(`${data.logReference}: ${JSON.stringify(data)}`)

			// Permanent failures — disable immediately
			// 50007 = Cannot send messages to this user (DMs disabled)
			// 10003 = Unknown Channel, 10013 = Unknown User
			const { code } = err
			if (code === 50007 || code === 10003 || code === 10013) {
				await this.query.updateQuery('humans', { enabled: 0 }, { id: data.target })
				this.logs.discord.warn(`${data.logReference}: #${this.id} Disabled user ${data.name} ${data.target} — Discord error ${code}`)
				this.consecutiveFails.delete(data.target)
				return true
			}

			// Disable user after repeated DM failures (they can re-enable with !poracle / /start)
			const fails = (this.consecutiveFails.get(data.target) || 0) + 1
			this.consecutiveFails.set(data.target, fails)
			const maxFails = this.config.tuning.maxSendFailsBeforeDisable || 5
			if (fails >= maxFails) {
				await this.query.updateQuery('humans', { enabled: 0 }, { id: data.target })
				this.logs.discord.warn(`${data.logReference}: #${this.id} Disabled user ${data.name} ${data.target} after ${fails} consecutive send failures`)
				this.consecutiveFails.delete(data.target)
			}
		}
		return true
	}

	async channelAlert(data) {
		try {
			const logReference = data.logReference ? data.logReference : 'Unknown'

			this.logs.discord.info(`${logReference}: #${this.id} -> ${data.name} ${data.target} CHANNEL Sending discord message${data.clean ? ' (clean)' : ''}`)
			const channel = await this.client.channels.fetch(data.target)
			let msgDeletionMs = this.config.discord.messageDeleteDelay || 0
			if (data.clean) {
				const tth = data.tth || {
					days: 0, hours: 0, minutes: 0, seconds: 0,
				}
				msgDeletionMs += ((tth.days * 86400) + (tth.hours * 3600) + (tth.minutes * 60) + tth.seconds) * 1000
			}
			if (!channel) return this.logs.discord.warn(`${logReference}: #${this.id} -> ${data.name} ${data.target} CHANNEL not found`)
			this.logs.discord.debug(`${logReference}: #${this.id} -> ${data.name} ${data.target} CHANNEL Sending discord message`, data.message)

			coerceEmbedColor(data.message.embed)
			if (data.message.embed) {
				data.message.embeds = [data.message.embed]
				delete data.message.embed
			}
			if (data.message.embeds) {
				for (const embed of data.message.embeds) coerceEmbedColor(embed)
			}

			if (this.config.discord.uploadEmbedImages && data.message.embeds && data.message.embeds.length && data.message.embeds[0].image && data.message.embeds[0].image.url) {
				const { url } = data.message.embeds[0].image
				data.message.embeds[0].image.url = 'attachment://map.png'
				data.message.files = [{ attachment: url, name: 'map.png' }]
			}

			const startTime = performance.now()
			const msg = await channel.send(data.message)
			const endTime = performance.now();
			(this.config.logger.timingStats ? this.logs.discord.verbose : this.logs.discord.debug)(`${logReference}: #${this.id} -> ${data.name} ${data.target} CHANNEL (${endTime - startTime} ms)`)
			metrics.discordDeliveryDuration.observe({ destination_type: 'channel' }, (endTime - startTime) / 1000)
			metrics.messagesSent.inc({ destination_type: 'discord:channel' })

			if (data.clean) {
				setTimeout(async () => {
					try {
						await msg.delete()
					} catch (err) {
						this.logs.discord.error(`${data.logReference}: #${this.id} Failed to send clean Discord alert to ${data.name} ${data.target}`, err)
					}
				}, msgDeletionMs)
				this.discordMessageTimeouts.set(msg.id, { type: 'channel', id: data.target }, Math.floor(msgDeletionMs / 1000) + 1)
			}
			return true
		} catch (err) {
			metrics.messagesFailed.inc({ destination_type: 'discord:channel' })
			const fails = (this.consecutiveFails.get(data.target) || 0) + 1
			this.consecutiveFails.set(data.target, fails)
			this.logs.discord.error(`${data.logReference}: #${this.id} -> ${data.name} ${data.target} CHANNEL failed to send Discord alert to `, err)
			this.logs.discord.error(`${data.logReference}: ${JSON.stringify(data)}`)
		}
		return true
	}

	async checkRole(guildID, users, roles) {
		const allUsers = users
		const validRoles = roles
		const invalidUsers = []
		let guild
		try {
			guild = await this.client.guilds.fetch(guildID)
		} catch (err) {
			if (err instanceof DiscordAPIError) {
				if (err.status === 403) {
					this.logs.log.debug(`${guildID} no access`)

					invalidUsers.push(...allUsers)
					return invalidUsers
				}
			} else {
				throw err
			}
		}
		for (const user of allUsers) {
			this.logs.log.debug(`Checking role for: ${user.name} - ${user.id}`)
			let discorduser
			try {
				discorduser = await guild.members.fetch(user.id)
			} catch (err) {
				if (err instanceof DiscordAPIError) {
					if (err.status === 404) {
						this.logs.log.debug(`${user.id} doesn't exist on guild ${guildID}`)
						invalidUsers.push(user)
						// eslint-disable-next-line no-continue
						continue
					}
				} else {
					throw err
				}
			}
			if (discorduser.roles.cache.find((r) => validRoles.includes(r.id))) {
				this.logs.log.debug(`${discorduser.user.username} has a valid role`)
			} else {
				this.logs.log.debug(`${discorduser.user.username} doesn't have a valid role`)
				invalidUsers.push(user)
			}
		}
		return invalidUsers
	}

	async saveTimeouts() {
		if (!this.client || !this.client.user || !this.client.user.username) return

		// eslint-disable-next-line no-underscore-dangle
		this.discordMessageTimeouts._checkData(false)
		return fsp.writeFile(`${CACHE_DIR}/cleancache-discord-${this.client.user.username}.json`, JSON.stringify(this.discordMessageTimeouts.data), 'utf8')
	}

	async loadTimeouts() {
		let loaddatatxt

		try {
			loaddatatxt = await fsp.readFile(`${CACHE_DIR}/cleancache-discord-${this.client.user.username}.json`, 'utf8')
		} catch {
			return
		}

		const now = Date.now()

		let data
		try {
			data = JSON.parse(loaddatatxt)
		} catch {
			this.logs.log.warn(`Clean cache for discord tag ${this.client.user.username} contains invalid data - ignoring`)
			return
		}

		for (const key of Object.keys(data)) {
			const msgData = data[key]
			let channel = null
			try {
				if (msgData.v.type === 'user') {
					const user = await this.client.users.fetch(msgData.v.id)
					channel = await user.createDM()
				}
				if (msgData.v.type === 'channel') {
					channel = await this.client.channels.fetch(msgData.v.id)
				}
				if (channel) {
					if (msgData.t <= now) {
						channel.messages.fetch(key).then((msg) => msg.delete()).catch(noop)
					} else {
						const newTtlms = Math.max(msgData.t - now, 2000)
						const newTtl = Math.floor(newTtlms / 1000)
						setTimeout(() => {
							channel.messages.fetch(key).then((msg) => msg.delete()).catch(noop)
						}, newTtlms)
						this.discordMessageTimeouts.set(key, msgData.v, newTtl)
					}
				}
			} catch (err) {
				this.logs.log.info(`Error processing historic deletes ${err}`)
			}
		}
	}
}

module.exports = Worker
