const axios = require('axios')

/**
 * Replace the ! prefix in commands with the platform-specific prefix.
 */
function replacePrefix(command, prefix) {
	if (!prefix || prefix === '!') return command
	return command.replace(/^!/gm, prefix)
}

/**
 * Sends text to the NLP parser and returns a formatted suggestion message,
 * or null if no useful suggestion was produced.
 *
 * @param {object} config - alerter config (needs config.processor.url, config.processor.headers)
 * @param {string} text - the user's raw text
 * @param {string} prefix - command prefix for this platform ('!' for Discord default, '/' for Telegram)
 * @returns {Promise<string|null>} suggestion message or null
 */
async function suggestCommand(config, text, prefix) {
	if (!text || !config.processor || !config.processor.url) return null

	let response
	try {
		response = await axios.post(`${config.processor.url}/api/ai/translate`, {
			message: text,
		}, {
			headers: {
				'Content-Type': 'application/json',
				...(config.processor.headers || {}),
			},
			timeout: 5000,
		})
	} catch {
		return null
	}

	const { data } = response
	if (!data) return null

	if (data.status === 'ambiguous' && data.options && data.options.length > 0) {
		let msg = 'Did you mean:\n'
		for (let i = 0; i < data.options.length; i++) {
			const cmd = replacePrefix(data.options[i].command, prefix)
			msg += `${i + 1}. ${data.options[i].label}: \`${cmd}\`\n`
		}
		msg += '\nCopy and paste the command you want.'
		return msg
	}

	if (data.status === 'ok' && data.command && !data.command.startsWith('ERROR:')) {
		const commands = data.command.split('\n').filter((c) => c.trim())
		if (commands.length === 0) return null

		let msg = 'Did you mean:\n'
		for (const cmd of commands) {
			msg += `\`${replacePrefix(cmd, prefix)}\`\n`
		}
		msg += '\nCopy and paste to run.'
		return msg
	}

	return null
}

module.exports = suggestCommand
