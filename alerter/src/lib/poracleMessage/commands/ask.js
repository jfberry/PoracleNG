const axios = require('axios')

exports.run = async (client, msg, args, options) => {
	try {
		if (!msg.isDM && !msg.isFromAdmin) {
			return
		}

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

		// Show the suggested command and ask for confirmation
		const commands = command.split('\n').filter((c) => c.trim())
		let preview = '**AI suggests:**\n'
		for (const cmd of commands) {
			preview += `\`${cmd}\`\n`
		}
		preview += '\nReact ✅ to run, or ❌ to cancel.'

		const confirmMsg = await msg.reply(preview)

		// Add reactions for confirmation
		if (confirmMsg && confirmMsg.react) {
			await confirmMsg.react('✅')
			await confirmMsg.react('❌')

			// Wait for reaction (60 seconds)
			const filter = (reaction, user) => ['✅', '❌'].includes(reaction.emoji.name) && user.id === msg.author.id
			try {
				const collected = await confirmMsg.awaitReactions({
					filter, max: 1, time: 60000, errors: ['time'],
				})
				const reaction = collected.first()

				if (reaction && reaction.emoji.name === '✅') {
					// Execute each command
					for (const cmd of commands) {
						const cmdArgs = cmd.trim().split(/\s+/)
						const cmdName = cmdArgs.shift().replace('!', '')

						// Route through the normal command system
						const handler = client.commands.get(cmdName)
						if (handler) {
							await handler.run(client, msg, cmdArgs, options)
						} else {
							await msg.reply(`Unknown command: \`${cmdName}\``)
						}
					}
				} else {
					await msg.reply('Cancelled.')
				}
			} catch {
				await msg.reply('No response — cancelled.')
			}
		} else {
			// Telegram or non-interactive — just show the commands
			await msg.reply(`To run these commands, copy and paste:\n${commands.map((c) => `\`${c}\``).join('\n')}`)
		}
	} catch (err) {
		client.log.error('ask command error:', err)
	}
}
