module.exports = async (fastify, options) => {
	fastify.post('/api/matched', options, async (req, reply) => {
		if (fastify.config.server.ipWhitelist.length && !fastify.config.server.ipWhitelist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} not in whitelist` }
		if (fastify.config.server.ipBlacklist.length && fastify.config.server.ipBlacklist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} in blacklist` }

		let data = req.body
		if (!Array.isArray(data)) data = [data]
		fastify.matchedQueue.push(...data)

		if (!reply.sent) return { webserver: 'happy' }
	})
}
