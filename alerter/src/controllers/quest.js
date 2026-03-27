const Controller = require('./controller')
const { log } = require('../lib/logger')

class Quest extends Controller {
	async handleMatched(obj, matchedUsers, matchedAreas) {
		const data = obj
		const minTth = this.config.general.alertMinimumTime || 0

		try {
			const logReference = data.pokestop_id

			Object.assign(data, this.config.general.dtsDictionary)

			// Map URLs are pre-computed by the Go processor enrichment
			data.googleMapUrl = data.googleMapUrl || `https://maps.google.com/maps?q=${data.latitude},${data.longitude}`
			data.appleMapUrl = data.appleMapUrl || `https://maps.apple.com/place?coordinate=${data.latitude},${data.longitude}`
			data.wazeMapUrl = data.wazeMapUrl || `https://www.waze.com/ul?ll=${data.latitude},${data.longitude}&navigate=yes&zoom=17`
			if (this.config.general.rdmURL) {
				data.rdmUrl = data.rdmUrl || `${this.config.general.rdmURL}${!this.config.general.rdmURL.endsWith('/') ? '/' : ''}@pokestop/${data.pokestop_id}`
			}
			if (this.config.general.reactMapURL) {
				data.reactMapUrl = data.reactMapUrl || `${this.config.general.reactMapURL}${!this.config.general.reactMapURL.endsWith('/') ? '/' : ''}id/pokestops/${data.pokestop_id}`
			}
			if (this.config.general.rocketMadURL) {
				data.rocketMadUrl = data.rocketMadUrl || `${this.config.general.rocketMadURL}${!this.config.general.rocketMadURL.endsWith('/') ? '/' : ''}?lat=${data.latitude}&lon=${data.longitude}&zoom=18.0`
			}
			data.intersection = await this.obtainIntersection(data)
			data.applemap = data.appleMapUrl // deprecated
			data.mapurl = data.googleMapUrl // deprecated
			data.disTime = data.disappearTime // deprecated

			if (data.pokestop_name) {
				data.pokestop_name = this.escapeJsonString(data.pokestop_name)
				data.pokestopName = data.pokestop_name
			}
			data.pokestop_url = data.pokestop_url || this.config.fallbacks?.pokestopUrl
			data.pokestopUrl = data.pokestop_url

			if (data.tth.firstDateWasLater || ((data.tth.hours * 3600) + (data.tth.minutes * 60) + data.tth.seconds) < minTth) {
				log.debug(`${data.pokestop_id}: [matched] quest already disappeared or is about to expire in: ${data.tth.hours}:${data.tth.minutes}:${data.tth.seconds}`)
				return []
			}

			// Use processor-provided matched areas
			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${logReference}: [matched] Quest appeared and ${whoCares.length} humans cared.`)
			} else {
				return []
			}

			// Icon URLs are pre-computed by the processor enrichment based on reward type
			data.imgUrl = data.imgUrl || this.config.fallbacks?.imgUrlPokestop
			data.imgUrlAlt = data.imgUrlAlt || this.config.fallbacks?.imgUrlPokestop

			const jobs = []

			// Static map is pre-computed by the processor enrichment
			data.staticmap = data.staticMap // deprecated alias

			// Future event fields are pre-computed by the Go processor enrichment

			for (const cares of whoCares) {
				this.log.debug(`${logReference}: [matched] Creating quest alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`)

				const language = cares.language || this.config.general.locale
				const translator = this.translatorFactory.Translator(language)
				let [platform] = cares.type.split(':')
				if (platform === 'webhook') platform = 'discord'

				// Per-language enrichment from processor — all quest/reward data pre-translated
				const langE = this.getLanguageEnrichment(data, language)

				// Quest title and reward data from processor enrichment
				data.questString = langE.questString || ''
				data.questStringEng = langE.questStringEng || ''
				data.monsterNames = langE.monsterNames || ''
				data.monsterNamesEng = langE.monsterNamesEng || ''
				data.itemNames = langE.itemNames || ''
				data.itemNamesEng = langE.itemNamesEng || ''
				data.energyMonstersNames = langE.energyMonstersNames || ''
				data.energyMonstersNamesEng = langE.energyMonstersNamesEng || ''
				data.candyMonstersNames = langE.candyMonstersNames || ''
				data.candyMonstersNamesEng = langE.candyMonstersNamesEng || ''
				data.rewardString = langE.rewardString || ''
				data.rewardStringEng = langE.rewardStringEng || ''

				// Structured reward data from processor enrichment
				if (langE.monsterList) data.monsterList = langE.monsterList
				if (langE.dustText) data.dustText = langE.dustText

				// Shiny emoji — resolve from processor-provided key
				data.shinyPossibleEmoji = ''
				if (langE.shinyPossibleEmojiKey) {
					data.shinyPossibleEmoji = translator.translate(this.emojiLookup.lookup(langE.shinyPossibleEmojiKey, platform))
				}

				const view = {
					...data,
					lat: +data.latitude.toFixed(4),
					lon: +data.longitude.toFixed(4),
					time: data.disappearTime,
					tthh: data.tth.hours,
					tthm: data.tth.minutes,
					tths: data.tth.seconds,
					confirmedTime: data.disappear_time_verified,
					now: new Date(),
					nowISO: new Date().toISOString(),
					areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
				}

				const templateType = 'quest'
				const message = await this.createMessage(logReference, templateType, platform, cares.template, language, cares.ping, view)

				const work = {
					lat: data.latitude.toString().substring(0, 8),
					lon: data.longitude.toString().substring(0, 8),
					message,
					target: cares.id,
					type: cares.type,
					name: cares.name,
					tth: data.tth,
					clean: cares.clean,
					logReference,
					language,
				}
				jobs.push(work)
			}

			return jobs
		} catch (e) {
			this.log.error(`${data.pokestop_id}: [matched] Can't seem to handle quest: `, e, data)
		}
	}
}

module.exports = Quest
