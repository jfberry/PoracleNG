const Controller = require('./controller')
const nightTime = require('./common/nightTime')

class Invasion extends Controller {
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
			data.name = data.name ? this.escapeJsonString(data.name) : this.escapeJsonString(data.pokestop_name)
			data.pokestopName = data.name
			data.url = data.url || this.config.fallbacks?.pokestopUrl
			data.pokestopUrl = data.url

			const incidentExpiration = data.incident_expiration ?? data.incident_expire_timestamp
			data.incidentExpiration = incidentExpiration
			// tth and disappearTime are pre-computed by the Go processor
			data.applemap = data.appleMapUrl // deprecated
			data.mapurl = data.googleMapUrl // deprecated
			data.distime = data.disappearTime // deprecated
			data.displayTypeId = data.display_type ?? data.incident_display_type ?? 0

			// Stop handling if it already disappeared or is about to go away
			if (data.tth.firstDateWasLater || ((data.tth.hours * 3600) + (data.tth.minutes * 60) + data.tth.seconds) < minTth) {
				this.log.debug(`${data.pokestop_id} [matched] Invasion already disappeared or is about to go away in: ${data.tth.hours}:${data.tth.minutes}:${data.tth.seconds}`)
				return []
			}

			// Use processor-provided matched areas
			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			// gruntTypeId resolution
			data.gruntTypeId = 0
			if (data.incident_grunt_type && (data.incident_grunt_type !== 352)) {
				data.gruntTypeId = data.incident_grunt_type
			} else if (data.grunt_type && (data.displayTypeId <= 6)) {
				data.gruntTypeId = data.grunt_type
			} else if (data.incident_grunt_type === 352) {
				data.grunt_type = 0
				data.displayTypeId = 8
			}

			// Defaults — processor enrichment provides these but set fallbacks
			data.gender = data.gruntGender || 0
			data.gruntName = ''
			data.gruntType = ''
			data.gruntTypeColor = data.gruntTypeColor || 'BABABA'
			data.gruntRewards = ''
			data.gruntRewardsList = {}

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${logReference}: [matched] Invasion at ${data.pokestopName} and ${whoCares.length} humans cared.`)
			} else {
				return []
			}

			try {
				// Icon URLs are pre-computed by the processor enrichment

				const jobs = []

				nightTime.setNightTime(data, this.config)

				data.intersection = await this.obtainIntersection(data)

				// Static map is pre-computed by the processor enrichment
				data.staticmap = data.staticMap // deprecated alias

				for (const cares of whoCares) {
					this.log.debug(`${logReference}: [matched] Creating invasion alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`, cares)

					const language = cares.language || this.config.general.locale
					const translator = this.translatorFactory.Translator(language)
					let [platform] = cares.type.split(':')
					if (platform === 'webhook') platform = 'discord'

					// Per-language enrichment from processor — all grunt data is pre-translated
					const langE = this.getLanguageEnrichment(data, language)

					// Weather
					data.gameWeatherName = langE.gameWeatherName || ''
					data.gameWeatherEmoji = langE.gameWeatherEmojiKey ? translator.translate(this.emojiLookup.lookup(langE.gameWeatherEmojiKey, platform)) : ''
					data.gameweather = data.gameWeatherName // deprecated
					data.gameweatheremoji = data.gameWeatherEmoji // deprecated

					// Grunt name and type — from processor per-language enrichment
					data.gruntName = langE.gruntName || ''
					data.gruntType = langE.gruntTypeName || ''

					// Grunt type emoji — resolve from processor-provided emoji key
					data.gruntTypeEmoji = ''
					if (langE.gruntTypeEmojiKey || data.gruntTypeEmojiKey) {
						const emojiKey = langE.gruntTypeEmojiKey || data.gruntTypeEmojiKey
						data.gruntTypeEmoji = translator.translate(this.emojiLookup.lookup(emojiKey, platform))
					} else {
						data.gruntTypeEmoji = translator.translate(this.emojiLookup.lookup('grunt-unknown', platform))
					}

					// Grunt type color — from processor base enrichment
					data.gruntTypeColor = data.gruntTypeColor || 'BABABA'

					// Gender — from processor per-language enrichment
					const genderName = langE.genderName || ''
					const genderEmojiKey = langE.genderEmojiKey || ''
					data.genderData = {
						name: genderName,
						emoji: genderEmojiKey ? translator.translate(this.emojiLookup.lookup(genderEmojiKey, platform)) : '',
					}

					// Rewards list — from processor per-language enrichment (pre-translated names)
					if (langE.gruntRewardsList) {
						data.gruntRewardsList = langE.gruntRewardsList
					}
					if (langE.gruntRewards) {
						data.gruntRewards = langE.gruntRewards
					}

					const view = {
						...data,
						time: data.distime,
						tthd: data.tth.days,
						tthh: data.tth.hours,
						tthm: data.tth.minutes,
						tths: data.tth.seconds,
						confirmedTime: data.disappear_time_verified,
						now: new Date(),
						nowISO: new Date().toISOString(),
						genderData: data.genderData,
						areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
					}

					const templateType = 'invasion'
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
						emoji: data.emoji,
						logReference,
						language,
					}
					jobs.push(work)
				}
				return jobs
			} catch (e) {
				this.log.error(`${data.pokestop_id}: [matched] Can't seem to handle pokestop (user cares): `, e, data)
				return []
			}
		} catch (e) {
			this.log.error(`${data.pokestop_id}: [matched] Can't seem to handle pokestop: `, e, data)
		}
	}
}

module.exports = Invasion
