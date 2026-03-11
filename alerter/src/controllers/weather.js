const moment = require('moment-timezone')
const { S2 } = require('s2-geometry')
const S2ts = require('nodes2ts')
require('moment-precise-range-plugin')
const Controller = require('./controller')

class Weather extends Controller {
	/**
	 * Handle a pre-matched weather change from the processor.
	 * matchedUsers: [{id, name, type, language, template, clean, ping}]
	 * matchedAreas: [{name, displayInMatches, group}]
	 */
	async handleMatched(obj, matchedUsers, matchedAreas) {
		let pregenerateTile = false
		const data = obj
		try {
			moment.locale(this.config.locale.timeformat)
			const logReference = data.s2_cell_id

			switch (this.config.geocoding.staticProvider.toLowerCase()) {
				case 'tileservercache': {
					pregenerateTile = true
					break
				}
				case 'google': {
					data.staticMap = `https://maps.googleapis.com/maps/api/staticmap?center=${data.latitude},${data.longitude}&markers=color:red|${data.latitude},${data.longitude}&maptype=${this.config.geocoding.type}&zoom=${this.config.geocoding.zoom}&size=${this.config.geocoding.width}x${this.config.geocoding.height}&key=${this.config.geocoding.staticKey[~~(this.config.geocoding.staticKey.length * Math.random())]}`
					break
				}
				case 'osm': {
					data.staticMap = `https://www.mapquestapi.com/staticmap/v5/map?locations=${data.latitude},${data.longitude}&size=${this.config.geocoding.width},${this.config.geocoding.height}&defaultMarker=marker-md-3B5998-22407F&zoom=${this.config.geocoding.zoom}&key=${this.config.geocoding.staticKey[~~(this.config.geocoding.staticKey.length * Math.random())]}`
					break
				}
				case 'mapbox': {
					data.staticMap = `https://api.mapbox.com/styles/v1/mapbox/streets-v10/static/url-https%3A%2F%2Fi.imgur.com%2FMK4NUzI.png(${data.longitude},${data.latitude})/${data.longitude},${data.latitude},${this.config.geocoding.zoom},0,0/${this.config.geocoding.width}x${this.config.geocoding.height}?access_token=${this.config.geocoding.staticKey[~~(this.config.geocoding.staticKey.length * Math.random())]}`
					break
				}
				default: {
					data.staticMap = ''
				}
			}

			const nowTimestamp = Math.floor(Date.now() / 1000)
			const currentHourTimestamp = nowTimestamp - (nowTimestamp % 3600)
			const nextHourTimestamp = currentHourTimestamp + 3600

			this.log.info(`${logReference}: Weather change received from processor: ${data.old_gameplay_condition} -> ${data.gameplay_condition} (source=${data.source})`)

			const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })

			require('./common/nightTime').setNightTime(data, moment(), this.config)

			if (pregenerateTile) {
				const tileServerOptions = this.tileserverPregen.getConfigForTileType('weather')
				if (tileServerOptions.type !== 'none') {
					data.staticMap = await this.tileserverPregen.getPregeneratedTileURL(logReference, 'weather', data, tileServerOptions.type)
				}
			}

			data.oldWeatherId = data.old_gameplay_condition > 0 ? data.old_gameplay_condition : ''
			data.oldWeatherNameEng = data.oldWeatherId ? this.GameData.utilData.weather[data.oldWeatherId].name : ''
			data.weatherId = data.gameplay_condition ? data.gameplay_condition : ''
			data.weatherNameEng = data.weatherId ? this.GameData.utilData.weather[data.weatherId].name : ''
			data.condition = data.gameplay_condition

			data.matchedAreas = matchedAreas.map((a) => ({ name: a.name, displayInMatches: a.displayInMatches, group: a.group }))
			data.matched = data.matchedAreas.map((x) => x.name.toLowerCase())
			if (this.imgUicons) data.imgUrl = await this.imgUicons.weatherIcon(data.condition) || this.config.fallbacks?.imgUrlWeather
			if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.weatherIcon(data.condition) || this.config.fallbacks?.imgUrlWeather
			if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.weatherIcon(data.condition)

			const jobs = []
			const now = moment.now()
			const weatherTth = moment.preciseDiff(now, nextHourTimestamp * 1000, true)

			for (const cares of matchedUsers) {
				this.log.debug(`${logReference}: Weather alert being generated for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`)

				const rateLimitTtr = this.getRateLimitTimeToRelease(cares.id)
				if (rateLimitTtr) {
					this.log.verbose(`${logReference}: Not creating weather alert (Rate limit) for ${cares.type} ${cares.id} ${cares.name} Time to release: ${rateLimitTtr}`)
					// eslint-disable-next-line no-continue
					continue
				}

				const language = cares.language || this.config.general.locale
				const translator = this.translatorFactory.Translator(language)
				let [platform] = cares.type.split(':')
				if (platform === 'webhook') platform = 'discord'

				data.oldWeatherName = data.oldWeatherNameEng ? translator.translate(data.oldWeatherNameEng) : ''
				data.oldWeatherEmojiEng = data.oldWeatherId ? this.emojiLookup.lookup(this.GameData.utilData.weather[data.oldWeatherId].emoji, platform) : ''
				data.oldWeatherEmoji = data.oldWeatherNameEng ? translator.translate(data.oldWeatherEmojiEng) : ''
				data.weatherCellId = data.s2_cell_id
				data.weatherName = data.weatherNameEng ? translator.translate(data.weatherNameEng) : ''
				data.weatherEmojiEng = data.weatherId ? this.emojiLookup.lookup(this.GameData.utilData.weather[data.weatherId].emoji, platform) : ''
				data.weatherEmoji = data.weatherEmojiEng ? translator.translate(data.weatherEmojiEng) : ''

				data.weather = data.weatherName // deprecated
				data.oldweather = data.oldWeatherName // deprecated
				data.oldweatheremoji = data.oldWeatherEmoji // deprecated
				data.weatheremoji = data.weatherEmoji // deprecated
				data.staticmap = data.staticMap // deprecated

				const view = {
					...data,
					...geoResult,
					id: data.s2_cell_id,
					areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
					now: new Date(),
					nowISO: new Date().toISOString(),
				}

				const templateType = 'weatherchange'
				const mustacheTemplate = this.getDts(logReference, templateType, platform, cares.template, language)
				let message
				if (mustacheTemplate) {
					let mustacheResult
					try {
						mustacheResult = mustacheTemplate(view, { data: { language, platform } })
					} catch (err) {
						this.log.error(`${logReference}: Error generating mustache results for ${platform}/${cares.template}/${language}`, err, view)
					}
					if (mustacheResult) {
						mustacheResult = await this.urlShorten(mustacheResult)
						try {
							message = JSON.parse(mustacheResult)
							if (cares.ping) {
								if (!message.content) {
									message.content = cares.ping
								} else {
									message.content += cares.ping
								}
							}
						} catch (err) {
							this.log.error(`${logReference}: Error JSON parsing mustache results ${mustacheResult}`, err)
						}
					}
				}

				if (!message) {
					message = { content: `*Poracle*: An alert was triggered with invalid or missing message template - ref: ${logReference}\nid: '${cares.template}' type: '${templateType}' platform: '${platform}' language: '${language}'` }
				}
				const work = {
					lat: data.latitude.toString().substring(0, 8),
					lon: data.longitude.toString().substring(0, 8),
					message,
					target: cares.id,
					type: cares.type,
					name: cares.name,
					tth: weatherTth,
					clean: cares.clean,
					emoji: [],
					logReference,
					language,
				}
				jobs.push(work)
			}

			this.log.info(`${logReference}: Weather change alert generated for ${jobs.length} humans`)
			return jobs
		} catch (e) {
			this.log.error(`Weather handleMatched error: `, e, data)
		}
	}
}

module.exports = Weather
