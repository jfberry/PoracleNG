const geoTz = require('geo-tz')
const moment = require('moment-timezone')
const axios = require('axios')
const { loadExampleJson, loadUserConfigJson } = require('../../configResolver')

exports.run = async (client, msg, args, options) => {
	try {
		if (!msg.isFromAdmin) return await msg.react('🙅')

		// Check target
		const util = client.createUtil(msg, options)

		const {
			canContinue, target,
		} = await util.buildTarget(args)

		if (!canContinue) return
		client.log.info(`${target.name}/${target.type}-${target.id}: ${__filename.slice(__dirname.length + 1, -3)} ${args}`)

		const human = await client.query.selectOneQuery('humans', { id: target.id })

		let template = client.config.general.defaultTemplateName?.toString() ?? '1'
		let language = client.config.general.locale

		const validHooks = ['pokemon', 'raid', 'pokestop', 'gym', 'nest', 'quest', 'fort-update', 'max-battle']

		const hookTypeDisplay = args[0]
		if (!validHooks.includes(hookTypeDisplay)) {
			await msg.reply('Hooks supported are: '.concat(validHooks.join(', ')))
			return
		}
		const hookType = hookTypeDisplay.replace(/-/g, '_')

		// Load bundled test data, then add user's custom entries on top
		const bundledTestdata = loadExampleJson('testdata.json') || []
		const userTestdata = loadUserConfigJson('testdata.json')
		const testdata = userTestdata ? [...bundledTestdata, ...userTestdata] : bundledTestdata

		const testId = args[1]

		if (!testId) {
			let message = `Tests found for hook type ${hookType}:\n\n`

			for (const test of testdata.filter((x) => x.type === hookType)) {
				message = message.concat(`  ${test.test}\n`)
			}

			return await msg.reply(message)
		}
		// Create extra command argument 'disapeardt' RegEx for 'poracle-test' command.
		const disapeardtReStr = '^(disapeardt):?([2][0][0-9][0-9]-([0][0-9]|[1][0-2])-([0-2][0-9]|[3][0-1])t([0-1][0-9]|[2][0-3]):[0-5][0-9]:[0-5][0-9])'
		const disapeardtRe = RegExp(disapeardtReStr, 'i')
		let disapearTimeOverwrite = null
		for (let i = args.length - 1; i >= 0; i--) {
			if (args[i].match(client.re.templateRe)) {
				[, , template] = args[i].match(client.re.templateRe)
				args.splice(i, 1)
			} else if (args[i].match(client.re.languageRe)) {
				[, , language] = args[i].match(client.re.languageRe)
				args.splice(i, 1)
			} else if (args[i].match(disapeardtRe)) {
				[, , disapearTimeOverwrite] = args[i].match(disapeardtRe)
				args.splice(i, 1)
			}
		}

		const dataItem = testdata.find((x) => x.type === hookType && x.test === testId)

		if (!dataItem) {
			await msg.reply(`Cannot find hook type ${hookType} test id ${testId}`)
			return
		}

		const hook = dataItem.webhook

		if (dataItem.location !== 'keep') {
			if (hook.latitude) hook.latitude = human.latitude
			if (hook.longitude) hook.longitude = human.longitude
		}

		// Freshen test data timestamps
		switch (hookType) {
			case 'pokemon': {
				if (disapearTimeOverwrite !== null) {
					const tz = geoTz.find(hook.latitude, hook.longitude)[0].toString()
					hook.disappear_time = moment.tz(disapearTimeOverwrite.toUpperCase(), tz).unix()
				} else {
					hook.disappear_time = Date.now() / 1000 + 10 * 60
				}
				break
			}
			case 'raid': {
				hook.start = Date.now() / 1000 + 10 * 60
				hook.end = hook.start + 30 * 60
				break
			}
			case 'pokestop': {
				if (hook.incident_expiration) hook.incident_expiration = Date.now() / 1000 + 10 * 60
				if (hook.incident_expire_timestamp) hook.incident_expire_timestamp = Date.now() / 1000 + 10 * 60
				if (hook.lure_expiration) hook.lure_expiration = Date.now() / 1000 + 5 * 60
				break
			}
			case 'quest': {
				break
			}
			case 'fort_update': {
				if (hook.old?.location) {
					hook.old.location.lat = human.latitude
					hook.old.location.lon = human.longitude
				}
				if (hook.new?.location) {
					hook.new.location.lat = human.latitude + 0.001
					hook.new.location.lon = human.longitude + 0.001
				}
				break
			}
			case 'gym': {
				break
			}
			case 'max_battle': {
				hook.battle_start = Date.now() / 1000 - 1 * 60
				hook.start_time = hook.battle_start
				hook.battle_end = Date.now() / 1000 + 120 * 60
				hook.end_time = hook.battle_end
				break
			}
			default:
		}

		await msg.reply(`Queueing ${hookType} test hook [${testId}] template [${template}]`)

		// Send to processor for enrichment via POST /api/test
		// The processor runs the webhook through the full enrichment pipeline
		// and sends the result back via /api/matched to the alerter
		const processorUrl = client.config.processor.url
		try {
			const response = await axios.post(`${processorUrl}/api/test`, {
				type: dataItem.type,
				webhook: hook,
				target: {
					id: target.id,
					name: target.name,
					type: target.type,
					language,
					template,
					latitude: human.latitude,
					longitude: human.longitude,
				},
			}, {
				headers: {
					'Content-Type': 'application/json',
					...(client.config.server.apiSecret ? { 'X-Poracle-Secret': client.config.server.apiSecret } : {}),
				},
				timeout: 10000,
			})
			if (response.data?.status !== 'ok') {
				client.log.warn(`poracle-test: processor returned: ${JSON.stringify(response.data)}`)
			}
		} catch (err) {
			client.log.error(`poracle-test: failed to POST to processor ${processorUrl}/api/test:`, err.message)
			await msg.reply(`Failed to send test to processor: ${err.message}`)
		}
	} catch (err) {
		client.log.error(`poracle-test command ${msg.content} unhappy:`, err)
	}
}
