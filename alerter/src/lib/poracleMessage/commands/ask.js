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
				await msg.reply('AI assistant is not configured. Ask an admin to enable it in config.toml.')
				return
			}
			client.log.error('AI translate request failed:', err.message)
			await msg.reply('Failed to reach AI assistant. Is the processor running?')
			return
		}

		const { data } = response
		if (data.status !== 'ok' || !data.command) {
			await msg.reply(`Sorry, I couldn't translate that: ${data.error || 'unknown error'}`)
			return
		}

		const { command } = data

		// Check if the AI returned an error message
		if (command.startsWith('ERROR:')) {
			await msg.reply(command)
			return
		}

		// Show the suggested commands for the user to copy and run
		const commands = command.split('\n').filter((c) => c.trim())
		let preview = '**AI suggests:**\n'
		for (const cmd of commands) {
			preview += `\`${cmd}\`\n`
		}
		preview += '\nCopy and paste to run.'

		await msg.reply(preview)
	} catch (err) {
		client.log.error('ask command error:', err)
	}
}
