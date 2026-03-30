module.exports = async (fastify, options) => {
	fastify.post('/api/postMessage', options, async (req, reply) => {
		fastify.logger.info(`API: ${req.ip} ${req.routeOptions.method} ${req.routeOptions.url}`)

		if (fastify.config.server.ipWhitelist.length && !fastify.config.server.ipWhitelist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} not in whitelist` }
		if (fastify.config.server.ipBlacklist.length && fastify.config.server.ipBlacklist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} in blacklist` }

		const secret = req.headers['x-poracle-secret']
		if (!secret || !fastify.config.server.apiSecret || secret !== fastify.config.server.apiSecret) {
			return { status: 'authError', reason: 'incorrect or missing api secret' }
		}

		if (!fastify.config.processor?.url) {
			return { status: 'error', message: 'processor URL not configured' }
		}

		let data = req.body
		if (!Array.isArray(data)) data = [data]

		data = data.map((x) => ({
			lat: x.lat || 0,
			lon: x.lon || 0,
			message: x.message,
			target: x.target,
			type: x.type,
			name: x.name || '',
			tth: x.tth || { hours: 1, minutes: 0, seconds: 0 },
			clean: !!x.clean,
			emoji: x.emoji || '',
			logReference: x.logReference || 'WebApi',
			language: x.language || fastify.config.general.locale,
		}))

		try {
			const axios = require('axios')
			await axios.post(`${fastify.config.processor.url}/api/deliverMessages`, data, { headers: fastify.config.processor.headers })
		} catch (err) {
			fastify.logger.error(`Failed to forward postMessage to processor: ${err.message}`)
			return { status: 'error', message: 'failed to forward to processor' }
		}

		if (!reply.sent) return { status: 'ok' }
	})
}
