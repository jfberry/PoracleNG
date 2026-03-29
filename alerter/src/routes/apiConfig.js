const { version } = require('../../package.json')

module.exports = async (fastify, options) => {
	fastify.get('/api/config/poracleWeb', options, async (req) => {
		fastify.logger.info(`API: ${req.ip} ${req.routeOptions.method} ${req.routeOptions.url}`)
		if (fastify.config.server.ipWhitelist.length && !fastify.config.server.ipWhitelist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} not in whitelist` }
		if (fastify.config.server.ipBlacklist.length && fastify.config.server.ipBlacklist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} in blacklist` }

		const secret = req.headers['x-poracle-secret']
		if (!secret || !fastify.config.server.apiSecret || secret !== fastify.config.server.apiSecret) {
			return { status: 'authError', reason: 'incorrect or missing api secret' }
		}

		return {
			status: 'ok',
			version,
			locale: fastify.config.general.locale,
			prefix: fastify.config.discord.prefix,
			providerURL: fastify.config.geocoding.providerURL,
			addressFormat: fastify.config.locale.addressFormat,
			staticKey: fastify.config.geocoding.staticKey,
			pvpFilterMaxRank: fastify.config.pvp.pvpFilterMaxRank,
			pvpFilterGreatMinCP: fastify.config.pvp.pvpFilterGreatMinCP,
			pvpFilterUltraMinCP: fastify.config.pvp.pvpFilterUltraMinCP,
			pvpFilterLittleMinCP: fastify.config.pvp.pvpFilterLittleMinCP,
			pvpLittleLeagueAllowed: true,
			pvpCaps: fastify.config.pvp.levelCaps ?? [50],
			pvpRequiresMinCp: fastify.config.pvp.forceMinCp && fastify.config.pvp.dataSource === 'webhook',
			defaultPvpCap: fastify.config.tracking.defaultUserTrackingLevelCap || 0,
			defaultTemplateName: fastify.config.general.defaultTemplateName || '1',
			channelNotesContainsCategory: fastify.config.discord.checkRole && fastify.config.reconciliation.discord.updateChannelNotes,
			admins: {
				discord: fastify.config.discord.admins,
				telegram: fastify.config.telegram.admins,
			},
			maxDistance: fastify.config.tracking.maxDistance,
			defaultDistance: fastify.config.tracking.defaultDistance,
			everythingFlagPermissions: fastify.config.tracking.everythingFlagPermissions,
			disabledHooks: ['Pokemon', 'Raid', 'Pokestop', 'Invasion', 'Lure', 'Quest', 'Weather', 'Nest', 'Gym', 'MaxBattle']
				.filter((hookType) => fastify.config.general[`disable${hookType}`]).map((hookType) => hookType.toLowerCase()),
			gymBattles: fastify.config.tracking.enableGymBattle ?? false,
		}
	})

}
