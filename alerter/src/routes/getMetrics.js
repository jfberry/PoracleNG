const { client } = require('../lib/metrics')

module.exports = async (fastify) => {
	fastify.get('/metrics', async (req, reply) => {
		reply.header('Content-Type', client.register.contentType)
		return client.register.metrics()
	})
}
