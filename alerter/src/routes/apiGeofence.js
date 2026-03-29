const fetch = require('node-fetch-native')

module.exports = async (fastify, options) => {
	// Geofence data and tile endpoints are now served by the processor.
	// Only the reload endpoint remains here as it triggers the processor reload.

	fastify.get('/api/geofence/reload', options, async (req) => {
		fastify.logger.info(`API: ${req.ip} ${req.routeOptions.method} ${req.routeOptions.url}`)

		if (fastify.config.server.ipWhitelist.length && !fastify.config.server.ipWhitelist.includes(req.ip)) {
			return {
				webserver: 'unhappy',
				reason: `ip ${req.ip} not in whitelist`,
			}
		}
		if (fastify.config.server.ipBlacklist.length && fastify.config.server.ipBlacklist.includes(req.ip)) {
			return {
				webserver: 'unhappy',
				reason: `ip ${req.ip} in blacklist`,
			}
		}

		const secret = req.headers['x-poracle-secret']
		if (!secret || !fastify.config.server.apiSecret || secret !== fastify.config.server.apiSecret) {
			return { status: 'authError', reason: 'incorrect or missing api secret' }
		}

		// Trigger processor geofence reload (re-fetches Koji geofences + reloads state)
		const processorUrl = fastify.config.processor?.url
		if (processorUrl) {
			try {
				const headers = fastify.config.processor?.headers || {}
				const res = await fetch(`${processorUrl}/api/geofence/reload`, { method: 'POST', headers })
				if (!res.ok) {
					fastify.logger.error(`Processor reload returned ${res.status}`)
					return { status: 'error', reason: `processor returned ${res.status}` }
				}
			} catch (err) {
				fastify.logger.error('Failed to trigger processor reload', err)
				return { status: 'error', reason: err.message }
			}
		}

		// Also reload alerter's local geofence data so commands see the new areas
		try {
			const geofenceLoader = require('../lib/geofenceLoader')
			const newGeofence = geofenceLoader.readAllGeofenceFiles(fastify.config)
			fastify.geofence.rbush = newGeofence.rbush
			fastify.geofence.geofence = newGeofence.geofence
			fastify.logger.info('Alerter geofence reloaded after processor reload')
		} catch (err) {
			fastify.logger.error('Failed to reload alerter geofence', err)
		}

		return { status: 'ok' }
	})
}
