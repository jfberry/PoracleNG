const geoTz = require('geo-tz')
const moment = require('moment-timezone')
require('moment-precise-range-plugin')

const Controller = require('./controller')
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
			data.googleMapUrl = `https://maps.google.com/maps?q=${data.latitude},${data.longitude}`
			data.appleMapUrl = `https://maps.apple.com/place?coordinate=${data.latitude},${data.longitude}`
			data.wazeMapUrl = `https://www.waze.com/ul?ll=${data.latitude},${data.longitude}&navigate=yes&zoom=17`
			if (this.config.general.rdmURL) {
				data.rdmUrl = `${this.config.general.rdmURL}${!this.config.general.rdmURL.endsWith('/') ? '/' : ''}@pokestop/${data.pokestop_id}`
			}
			if (this.config.general.reactMapURL) {
				data.reactMapUrl = `${this.config.general.reactMapURL}${!this.config.general.reactMapURL.endsWith('/') ? '/' : ''}id/pokestops/${data.pokestop_id}`
			}
			if (this.config.general.rocketMadURL) {
				data.rocketMadUrl = `${this.config.general.rocketMadURL}${!this.config.general.rocketMadURL.endsWith('/') ? '/' : ''}?lat=${data.latitude}&lon=${data.longitude}&zoom=18.0`
			}
			data.name = data.name ? this.escapeJsonString(data.name) : this.escapeJsonString(data.pokestop_name)
			data.pokestopName = data.name
			data.url = data.url || this.config.fallbacks?.pokestopUrl
			data.pokestopUrl = data.url

			const lureExpiration = data.lure_expiration
			data.tth = moment.preciseDiff(Date.now(), lureExpiration * 1000, true)
			const disappearTime = moment(lureExpiration * 1000).tz(geoTz.find(data.latitude, data.longitude)[0].toString())
			data.disappearTime = disappearTime.format(this.config.locale.time)
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

			data.lureTypeColor = this.GameData.utilData.lures[data.lure_id].color
			data.lureTypeNameEng = this.GameData.utilData.lures[data.lure_id].name

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${logReference}: [matched] Lure of type ${data.lureTypeNameEng} at ${data.pokestopName} and ${whoCares.length} humans cared.`)
			} else {
				return []
			}

			let discordCacheBad = true
			whoCares.forEach((cares) => {
				if (!this.isRateLimited(cares.id)) discordCacheBad = false
			})
			if (discordCacheBad) return []

			setImmediate(async () => {
				try {
					if (this.imgUicons) data.imgUrl = await this.imgUicons.pokestopIcon(data.lureTypeId) || this.config.fallbacks?.imgUrlPokestop
					if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.pokestopIcon(data.lureTypeId) || this.config.fallbacks?.imgUrlPokestop
					if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.pokestopIcon(data.lureTypeId)

					const geoResult = await this.getAddress({
						lat: data.latitude,
						lon: data.longitude,
					})
					const jobs = []

					require('./common/nightTime').setNightTime(data, disappearTime, this.config)

					await this.getStaticMapUrl(logReference, data, 'pokestop', ['latitude', 'longitude', 'imgUrl', 'lureTypeId', 'style'])
					data.intersection = await this.obtainIntersection(data)

					data.staticmap = data.staticMap // deprecated

					for (const cares of whoCares) {
						this.log.debug(`${logReference}: [matched] Creating lure alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`, cares)

						const rateLimitTtr = this.getRateLimitTimeToRelease(cares.id)
						if (rateLimitTtr) {
							this.log.verbose(`${logReference}: [matched] Not creating lure alert (Rate limit) for ${cares.type} ${cares.id} ${cares.name} Time to release: ${rateLimitTtr}`)
							// eslint-disable-next-line no-continue
							continue
						}
						this.log.verbose(`${logReference}: [matched] Creating lure alert for ${cares.type} ${cares.id} ${cares.name} ${cares.language} ${cares.template}`)

						const language = cares.language || this.config.general.locale
						const translator = this.translatorFactory.Translator(language)
						let [platform] = cares.type.split(':')
						if (platform === 'webhook') platform = 'discord'

						// full build
						data.lureTypeName = translator.translate(data.lureTypeNameEng)
						data.lureType = data.lureTypeName
						data.lureTypeEmoji = this.emojiLookup.lookup(this.GameData.utilData.lures[data.lure_id].emoji, platform)

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
					this.emit('postMessage', jobs)
				} catch (e) {
					this.log.error(`${data.pokestop_id}: [matched] Can't seem to handle pokestop(lure) (user cares): `, e, data)
				}
			})

			return []
		} catch (e) {
			this.log.error(`${data.pokestop_id}: [matched] Can't seem to handle pokestop(lure): `, e, data)
		}
	}
}

module.exports = Lure
