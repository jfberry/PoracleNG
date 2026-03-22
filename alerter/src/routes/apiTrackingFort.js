const { diff } = require('deep-object-diff')

const trackedCommand = require('../lib/poracleMessage/commands/tracked')

module.exports = async (fastify, options) => {
	fastify.get('/api/tracking/fort/:id', options, async (req) => {
		fastify.logger.info(`API: ${req.ip} ${req.routeOptions.method} ${req.routeOptions.url}`)

		if (fastify.config.server.ipWhitelist.length && !fastify.config.server.ipWhitelist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} not in whitelist` }
		if (fastify.config.server.ipBlacklist.length && fastify.config.server.ipBlacklist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} in blacklist` }

		const secret = req.headers['x-poracle-secret']
		if (!secret || !fastify.config.server.apiSecret || secret !== fastify.config.server.apiSecret) {
			return { status: 'authError', reason: 'incorrect or missing api secret' }
		}
		const human = await fastify.query.selectOneQuery('humans', { id: req.params.id })

		if (!human) {
			return {
				status: 'error',
				message: 'User not found',
			}
		}
		const language = human.language || fastify.config.general.locale
		const translator = fastify.translatorFactory.Translator(language)

		const forts = await fastify.query.selectAllQuery('forts', { id: req.params.id, profile_no: req.query.profile_no || human.current_profile_no })

		const fortWithDesc = forts.map((row) => ({ ...row, description: trackedCommand.fortUpdateRowText(fastify.config, translator, fastify.GameData, row) }))

		return {
			status: 'ok',
			fort: fortWithDesc,
		}
	})

	fastify.delete('/api/tracking/fort/:id/byUid/:uid', options, async (req) => {
		fastify.logger.info(`API: ${req.ip} ${req.routeOptions.method} ${req.routeOptions.url}`)

		if (fastify.config.server.ipWhitelist.length && !fastify.config.server.ipWhitelist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} not in whitelist` }
		if (fastify.config.server.ipBlacklist.length && fastify.config.server.ipBlacklist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} in blacklist` }

		const secret = req.headers['x-poracle-secret']
		if (!secret || !fastify.config.server.apiSecret || secret !== fastify.config.server.apiSecret) {
			return { status: 'authError', reason: 'incorrect or missing api secret' }
		}

		await fastify.query.deleteQuery('forts', { id: req.params.id, uid: req.params.uid })
		if (fastify.triggerReloadAlerts) fastify.triggerReloadAlerts()

		return {
			status: 'ok',
		}
	})

	fastify.post('/api/tracking/fort/:id', options, async (req) => {
		fastify.logger.info(`API: ${req.ip} ${req.routeOptions.method} ${req.routeOptions.url}`)

		if (fastify.config.server.ipWhitelist.length && !fastify.config.server.ipWhitelist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} not in whitelist` }
		if (fastify.config.server.ipBlacklist.length && fastify.config.server.ipBlacklist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} in blacklist` }

		const secret = req.headers['x-poracle-secret']
		if (!secret || !fastify.config.server.apiSecret || secret !== fastify.config.server.apiSecret) {
			return { status: 'authError', reason: 'incorrect or missing api secret' }
		}

		const human = await fastify.query.selectOneQuery('humans', { id: req.params.id })

		if (!human) {
			return {
				status: 'error',
				message: 'User not found',
			}
		}

		const language = human.language || fastify.config.general.locale
		const translator = fastify.translatorFactory.Translator(language)
		const { id } = req.params
		const currentProfileNo = req.query.profile_no || human.current_profile_no
		const silent = req.query.silent || req.query.suppressMessage

		let insertReq = req.body
		if (!Array.isArray(insertReq)) insertReq = [insertReq]

		const defaultTo = ((value, x) => ((value === undefined) ? x : value))

		const validFortTypes = ['pokestop', 'gym', 'everything']

		const insert = []
		for (const row of insertReq) {
			const fortType = row.fort_type || 'everything'
			if (!validFortTypes.includes(fortType)) {
				return { status: 'error', message: `Invalid fort_type: ${fortType} (must be pokestop, gym, or everything)` }
			}

			let changeTypes = row.change_types
			if (Array.isArray(changeTypes)) {
				changeTypes = JSON.stringify(changeTypes)
			} else if (changeTypes === undefined || changeTypes === null) {
				changeTypes = '[]'
			}

			insert.push({
				id,
				profile_no: currentProfileNo,
				ping: '',
				template: (row.template || fastify.config.general.defaultTemplateName).toString(),
				distance: +defaultTo(row.distance, 0),
				fort_type: fortType,
				include_empty: !!defaultTo(row.include_empty, false),
				change_types: changeTypes,
			})
		}

		try {
			const tracked = await fastify.query.selectAllQuery('forts', { id, profile_no: currentProfileNo })

			const updates = []
			const alreadyPresent = []

			for (let i = insert.length - 1; i >= 0; i--) {
				const toInsert = insert[i]

				for (const existing of tracked.filter((x) => x.fort_type === toInsert.fort_type)) {
					const differences = diff(existing, toInsert)

					switch (Object.keys(differences).length) {
						case 1:		// No differences (only UID)
							alreadyPresent.push(toInsert)
							insert.splice(i, 1)
							break
						case 2:		// One difference (something + uid)
							if (Object.keys(differences).some((x) => ['distance', 'template', 'include_empty', 'change_types'].includes(x))) {
								updates.push({
									...toInsert,
									uid: existing.uid,
								})
								insert.splice(i, 1)
							}
							break
						default:	// more differences
							break
					}
				}
			}

			let message = ''

			if ((alreadyPresent.length + updates.length + insert.length) > 50) {
				message = translator.translateFormat('I have made a lot of changes. See {0}{1} for details', '!', /* util.prefix, */ translator.translate('tracked'))
			} else {
				for (const i of alreadyPresent) {
					message = message.concat(translator.translate('Unchanged: '), trackedCommand.fortUpdateRowText(fastify.config, translator, fastify.GameData, i), '\n')
				}
				for (const i of updates) {
					message = message.concat(translator.translate('Updated: '), trackedCommand.fortUpdateRowText(fastify.config, translator, fastify.GameData, i), '\n')
				}
				for (const i of insert) {
					message = message.concat(translator.translate('New: '), trackedCommand.fortUpdateRowText(fastify.config, translator, fastify.GameData, i), '\n')
				}
			}

			await fastify.query.deleteWhereInQuery(
				'forts',
				{
					id,
					profile_no: currentProfileNo,
				},
				updates.map((x) => x.uid),
				'uid',
			)

			const insertResult = await fastify.query.insertQuery('forts', [...insert, ...updates], 'uid')
			const newUids = Array.isArray(insertResult) ? insertResult.map((r) => (typeof r === 'object' ? r.uid : r)) : []
			if (fastify.triggerReloadAlerts) fastify.triggerReloadAlerts()

			if (!silent) {
				const data = [{
					lat: 0,
					lon: 0,
					message: { content: message },
					target: human.id,
					type: human.type,
					name: human.name,
					tth: { hours: 1, minutes: 0, seconds: 0 },
					clean: false,
					emoji: '',
					logReference: 'WebApi',
					language,
				}]

				data.forEach((job) => {
					if (['discord:user', 'discord:channel', 'webhook'].includes(job.type)) fastify.discordQueue.push(job)
					if (['telegram:user', 'telegram:channel'].includes(job.type)) fastify.telegramQueue.push(job)
				})
			}

			return {
				status: 'ok',
				message: silent ? '' : message,
				newUids,
				alreadyPresent: alreadyPresent.length,
				updates: updates.length,
				insert: insert.length,
			}
		} catch (err) {
			fastify.logger.error(`API: ${req.ip} ${req.routeOptions.method} ${req.routeOptions.url}`, err)
			return {
				status: 'error',
				message: 'Exception raised during execution',
			}
		}
	})

	fastify.post('/api/tracking/fort/:id/delete', options, async (req) => {
		fastify.logger.info(`API: ${req.ip} ${req.routeOptions.method} ${req.routeOptions.url}`)

		if (fastify.config.server.ipWhitelist.length && !fastify.config.server.ipWhitelist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} not in whitelist` }
		if (fastify.config.server.ipBlacklist.length && fastify.config.server.ipBlacklist.includes(req.ip)) return { webserver: 'unhappy', reason: `ip ${req.ip} in blacklist` }

		const secret = req.headers['x-poracle-secret']
		if (!secret || !fastify.config.server.apiSecret || secret !== fastify.config.server.apiSecret) {
			return { status: 'authError', reason: 'incorrect or missing api secret' }
		}

		let deleteUids = req.body
		if (!Array.isArray(deleteUids)) deleteUids = [deleteUids]

		await fastify.query.deleteWhereInQuery('forts', {
			id: req.params.id,
		}, deleteUids, 'uid')
		if (fastify.triggerReloadAlerts) fastify.triggerReloadAlerts()

		return {
			status: 'ok',
		}
	})
}
