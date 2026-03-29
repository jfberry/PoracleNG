const axios = require('axios')

async function renderTemplate(client, type, id, platform, language, view) {
	if (!client.config.processor.url) return null

	try {
		const resp = await axios.post(`${client.config.processor.url}/api/dts/render`, {
			type,
			id: id || '',
			platform,
			language,
			view: view || {},
		}, { headers: { 'Content-Type': 'application/json', ...client.config.processor.headers }, timeout: 5000 })

		if (resp.data.status === 'ok' && resp.data.message) {
			return resp.data.message
		}
	} catch (err) {
		if (err.response && err.response.status === 404) {
			return null
		}
		client.log.warn(`DTS render failed for ${type}/${id}: ${err.message}`)
	}
	return null
}

async function provideSingleLineHelp(client, msg, util, language, target, commandName) {
	const translator = client.translatorFactory.Translator(language)

	let platform = target.type.split(':')[0]
	if (platform === 'webhook') platform = 'discord'

	const helpAvailable = await renderTemplate(client, 'help', commandName, platform, language, {})
	if (helpAvailable) {
		await msg.reply(translator.translateFormat('Use `{0}{1} {2}` for more details on this command', util.prefix, translator.translate('help'), translator.translate(commandName)), { style: 'markdown' })
	} else {
		await msg.reply(translator.translateFormat('Use `{0}{1}` for more help', util.prefix, translator.translate('help')), { style: 'markdown' })
	}
}

exports.provideSingleLineHelp = provideSingleLineHelp

exports.run = async (client, msg, args, options) => {
	try {
		// Check target
		const util = client.createUtil(msg, options)

		const {
			canContinue, target, language,
		} = await util.buildTarget(args)

		if (!canContinue) return
		client.log.info(`${target.name}/${target.type}-${target.id}: ${__filename.slice(__dirname.length + 1, -3)} ${args}`)

		let helpLanguage = language
		if (client.config.general.availableLanguages) {
			for (const [key, availableLanguage] of Object.entries(client.config.general.availableLanguages)) {
				if (availableLanguage.help === msg.command) {
					helpLanguage = key
					break
				}
			}
		}

		const human = await client.query.selectOneQuery('humans', { id: target.id })

		if (human && !human.language) {
			await client.query.updateQuery('humans', { language: helpLanguage }, { id: target.id })
		}

		let platform = target.type.split(':')[0]
		if (platform === 'webhook') platform = 'discord'

		const view = { prefix: util.prefix }
		let message

		if (args[0]) {
			message = await renderTemplate(client, 'help', args[0], platform, helpLanguage, view)
		} else {
			message = await renderTemplate(client, 'greeting', '', platform, helpLanguage, view)
		}

		if (!message) {
			await msg.react('\u{1F645}')
			return
		}

		if (message.embed) {
			if (message.embed.title) message.embed.title = ''
			if (message.embed.description) message.embed.description = ''
		}

		if (platform === 'discord') {
			await msg.reply(message)
		} else {
			let messageText = ''
			const fields = (message.embed && message.embed.fields) || []

			for (const field of fields) {
				const fieldLine = `\n\n${field.name}\n\n${field.value}`
				if (messageText.length + fieldLine.length > 1024) {
					await msg.reply(messageText, { style: 'markdown' })
					messageText = ''
				}
				messageText = messageText.concat(fieldLine)
			}
			if (messageText) {
				await msg.reply(messageText, { style: 'markdown' })
			}
		}
	} catch (err) {
		client.log.error('help command unhappy:', err)
	}
}
