module.exports = async (fastify, options) => {
	fastify.post('/api/deliverMessages', options, async (req, reply) => {
		if (fastify.config.server.ipWhitelist.length && !fastify.config.server.ipWhitelist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} not in whitelist` }
		if (fastify.config.server.ipBlacklist.length && fastify.config.server.ipBlacklist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} in blacklist` }

		const secret = req.headers['x-poracle-secret']
		if (fastify.config.server.apiSecret && (!secret || secret !== fastify.config.server.apiSecret)) {
			return { status: 'authError', reason: 'incorrect or missing api secret' }
		}

		const jobs = req.body
		if (!Array.isArray(jobs)) {
			return reply.code(400).send({ status: 'error', message: 'expected array' })
		}

		let queued = 0
		for (const job of jobs) {
			if (!job.target || !job.type || !job.message) continue

			if (['discord:user', 'discord:channel', 'webhook'].includes(job.type)) {
				fastify.discordQueue.push(job)
				queued++
			} else if (['telegram:user', 'telegram:channel', 'telegram:group'].includes(job.type)) {
				fastify.telegramQueue.push(job)
				queued++
			}
		}

		return { status: 'ok', queued }
	})
}
