const Controller = require('./controller')
const nightTime = require('./common/nightTime')

/**
 * Controller for processing pokestop webhooks
 * Alerts on lures
 */
class Lure extends Controller {
	async handleMatched(obj, matchedUsers, matchedAreas) {
		const data = obj
		const minTth = this.config.general.alertMinimumTime || 0

		try {
			const logReference = data.pokestop_id

			Object.assign(data, this.config.general.dtsDictionary)
			data.name = data.name ? this.escapeJsonString(data.name) : this.escapeJsonString(data.pokestop_name)
			data.pokestopName = data.name
			data.url = data.url || this.config.fallbacks?.pokestopUrl
			data.pokestopUrl = data.url

			// tth and disappearTime provided by processor enrichment
			data.applemap = data.appleMapUrl // deprecated
			data.mapurl = data.googleMapUrl // deprecated
			data.distime = data.disappearTime // deprecated

			// Stop handling if it already disappeared or is about to go away
			if (data.tth.firstDateWasLater || ((data.tth.hours * 3600) + (data.tth.minutes * 60) + data.tth.seconds) < minTth) {
				this.log.debug(`${data.pokestop_id} [matched] Lure already disappeared or is about to go away in: ${data.tth.hours}:${data.tth.minutes}:${data.tth.seconds}`)
				return []
			}

			// Use processor-provided matched areas
			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			data.lureTypeId = 0
			if (data.lure_id) {
				data.lureTypeId = data.lure_id
			}

			// lureColor and lureEmojiKey provided by processor base enrichment
			data.lureTypeColor = data.lureColor

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${logReference}: [matched] Lure at ${data.pokestopName} and ${whoCares.length} humans cared.`)
			} else {
				return []
			}

			try {
				// Icon URLs are pre-computed by the processor enrichment

				const geoResult = await this.getAddress({
					lat: data.latitude,
					lon: data.longitude,
				})
				const jobs = []

				nightTime.setNightTime(data, this.config)

				await this.getStaticMapUrl(logReference, data, 'pokestop', ['latitude', 'longitude', 'imgUrl', 'lureTypeId', 'style'])
				data.intersection = await this.obtainIntersection(data)

				data.staticmap = data.staticMap // deprecated

				for (const cares of whoCares) {
					this.log.debug(`${logReference}: [matched] Creating lure alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`, cares)

					this.log.verbose(`${logReference}: [matched] Creating lure alert for ${cares.type} ${cares.id} ${cares.name} ${cares.language} ${cares.template}`)

					const language = cares.language || this.config.general.locale
					let [platform] = cares.type.split(':')
					if (platform === 'webhook') platform = 'discord'

					// Pre-translated names from processor per-language enrichment
					const langEnrichment = this.getLanguageEnrichment(data, language)
					data.lureTypeName = langEnrichment.lureTypeName
					data.lureType = data.lureTypeName

					// Per-platform emoji lookup (using emoji key from base enrichment)
					data.lureTypeEmoji = data.lureEmojiKey ? this.emojiLookup.lookup(data.lureEmojiKey, platform) : ''

					const view = {
						...geoResult,
						...data,
						time: data.distime,
						tthh: data.tth.hours,
						tthm: data.tth.minutes,
						tths: data.tth.seconds,
						now: new Date(),
						nowISO: new Date().toISOString(),
						areas: data.matchedAreas.filter((area) => area.displayInMatches)
							.map((area) => area.name)
							.join(', '),
					}

					const templateType = 'lure'
					const message = await this.createMessage(logReference, templateType, platform, cares.template, language, cares.ping, view)

					const work = {
						lat: data.latitude.toString()
							.substring(0, 8),
						lon: data.longitude.toString()
							.substring(0, 8),
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
				this.log.error(`${data.pokestop_id}: [matched] Can't seem to handle pokestop(lure) (user cares): `, e, data)
				return []
			}
		} catch (e) {
			this.log.error(`${data.pokestop_id}: [matched] Can't seem to handle pokestop(lure): `, e, data)
		}
	}
}

module.exports = Lure
