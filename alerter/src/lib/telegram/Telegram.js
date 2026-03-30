const { EventEmitter } = require('events')
const fs = require('fs')
const emojiStrip = require('../../util/emojiStrip')

class Telegram extends EventEmitter {
	constructor(id, config, logs, GameData, PoracleInfo, geofence, controller, query, scannerQuery, telegraf, translatorFactory, commandParser, re) {
		super()
		this.config = config
		this.logs = logs
		this.GameData = GameData
		this.PoracleInfo = PoracleInfo
		this.geofence = geofence
		this.translatorFactory = translatorFactory
		this.translator = translatorFactory.default
		this.commandParser = commandParser
		this.tempProps = {}
		this.controller = controller
		this.enabledCommands = []
		this.client = {}
		this.query = query
		this.commandFiles = fs.readdirSync(`${__dirname}/commands`)
		this.bot = telegraf
		this.id = id
		this.bot
			.use(commandParser(this.translatorFactory))
			.use(controller(query, scannerQuery, logs, GameData, PoracleInfo, geofence, config, re, translatorFactory, emojiStrip))

		this.commands = {}
	}

	start() {
		// Handle identify special case on channels & in conversations

		this.bot.on('channel_post', (ctx, next) => {
			if (ctx.update.channel_post
				&& ctx.update.channel_post.text
				&& ctx.update.channel_post.text.startsWith('/identify')) {
				ctx.reply(`This channel is id: [ ${ctx.update.channel_post.chat.id} ] and your id is: unknown - this is a channel (and can't be used for bot registration)`)
			}
			return next()
		})

		this.bot.hears(/^\/identify/, (ctx) => {
			if (ctx.update.message.chat.type === 'private') {
				ctx.reply(`This is a private message and your id is: [ ${ctx.update.message.from.id} ]`)
			} else {
				ctx.reply(`This channel is id: [ ${ctx.update.message.chat.id} ] and your id is: [ ${ctx.update.message.from.id} ]`)
			}
		})

		/* load available commands into command structure */
		this.commandFiles.map((file) => {
			if (!file.endsWith('.js')) return
			this.tempProps = require(`${__dirname}/commands/${file}`) // eslint-disable-line global-require
			const commandName = file.split('.')[0]
			if (!this.config.general.disabledCommands.includes(commandName)) {
				this.enabledCommands.push(commandName)
				this.commands[commandName] = this.tempProps

				const translatedCommands = this.translatorFactory.translateCommand(commandName)
				for (const translatedCommand of translatedCommands) {
					if (translatedCommand !== commandName) {
						this.enabledCommands.push(translatedCommand)
						this.commands[translatedCommand] = this.tempProps
					}
				}
			}
		})

		/* install extra middleware for telegram location sharing function, because .command(...) only catch text type messages */
		if (!this.config.general.disabledCommands.includes('location')) {
			const locationHandler = require(`${__dirname}/commands/location`)
			this.bot.on('location', locationHandler)
		}

		if (this.config.general.availableLanguages && !this.config.general.disabledCommands.includes('poracle')) {
			for (const [, availableLanguage] of Object.entries(this.config.general.availableLanguages)) {
				const commandName = availableLanguage.poracle
				if (commandName && !this.enabledCommands.includes(commandName)) {
					const props = require(`${__dirname}/commands/poracle`)
					this.enabledCommands.push(commandName)
					this.commands[commandName] = props
				}
			}
		}

		// use 'hears' to launch our command processor rather than bot commands
		this.bot.on('text', async (ctx) => this.processCommand(ctx))

		this.bot.catch((err, ctx) => {
			this.logs.log.error(`Ooops, encountered an error for ${ctx.updateType}`, err)
		})
		this.bot.start(() => {
			throw new Error('Telegraf error')
		})
		this.logs.log.info(`Telegram commando loaded ${this.enabledCommands.join(', ')} commands`)
		this.bot.launch()
	}

	async processCommand(ctx) {
		const { command } = ctx.state
		if (!command) return
		if (command.bot && command.bot.toLowerCase() !== ctx.botInfo.username.toLowerCase()) return
		if (Object.keys(this.commands).includes(command.command)) {
			ctx.poracleAddMessageQueue = (queue) => this.emit('sendMessages', queue)
			ctx.poracleAddMatchedQueue = (queue) => this.emit('addMatchedQueue', queue)
			ctx.poracleReloadAlerts = () => this.emit('refreshAlertCache')

			return this.commands[command.command](ctx)
		}
		if (ctx.update.message.chat.type === 'private'
				&& this.config.telegram.unrecognisedCommandMessage) {
			ctx.reply(this.config.telegram.unrecognisedCommandMessage)
		}
	}
}

module.exports = Telegram
