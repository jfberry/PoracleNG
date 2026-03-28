const axios = require('axios')

exports.run = async (client, msg, args) => {
	try {
		const question = args.join(' ').trim()
		if (!question) {
			await msg.reply('Usage: `!ask <what you want to track in plain English>`\nExample: `!ask track shiny pikachu with good PVP`')
			return
		}

		const processorUrl = `${client.config.processor.url}/api/ai/translate`

		let response
		try {
			response = await axios.post(processorUrl, {
				message: question,
			}, {
				headers: {
					'Content-Type': 'application/json',
					...(client.config.processor.headers || {}),
				},
				timeout: 30000,
			})
		} catch (err) {
			if (err.response && err.response.status === 503) {
				await msg.reply('`!ask` is not configured. Ask an admin to enable `[ai] enabled = true` in config.toml.')
				return
			}
			client.log.error('ask translate request failed:', err.message)
			await msg.reply('Failed to reach the processor. Is it running?')
			return
		}

		const { data } = response

		// Ambiguous: show numbered options
		if (data.status === 'ambiguous') {
			let preview = `${data.message || 'Did you mean:'}\n`
			for (let i = 0; i < data.options.length; i++) {
				preview += `${i + 1}. ${data.options[i].label}: \`${data.options[i].command}\`\n`
			}
			preview += '\nCopy and paste the command you want.'
			await msg.reply(preview)
			return
		}

		// Error
		if (data.status !== 'ok' || !data.command) {
			await msg.reply(`Sorry, I couldn't translate that: ${data.error || 'unknown error'}`)
			return
		}

		const { command } = data

		if (command.startsWith('ERROR:')) {
			await msg.reply(command)
			return
		}

		// Show suggested commands
		const commands = command.split('\n').filter((c) => c.trim())
		let preview = '**Suggested command(s):**\n'
		for (const cmd of commands) {
			preview += `\`${cmd}\`\n`
		}
		preview += '\nCopy and paste to run.'

		await msg.reply(preview)
	} catch (err) {
		client.log.error('ask command error:', err)
	}
}
