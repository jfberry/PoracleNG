const Controller = require('./controller')

/**
 * Controller for processing nest webhooks
 */
class Nest extends Controller {
	async handleMatched(obj, matchedUsers, matchedAreas) {
		const data = obj

		try {
			const logReference = data.nest_id

			data.longitude = data.lon
			data.latitude = data.lat

			Object.assign(data, this.config.general.dtsDictionary)
			data.name = this.escapeJsonString(data.name)

			// tth, disappearDate, disappearTime, resetDate, resetTime provided by processor enrichment

			data.applemap = data.appleMapUrl // deprecated
			data.mapurl = data.googleMapUrl // deprecated
			data.distime = data.disappearTime // deprecated

			// Use processor-provided matched areas
			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			data.form ??= 0
			data.nestName = this.escapeJsonString(data.name)
			data.pokemonId = data.pokemon_id
			data.formId = data.form
			data.pokemonCount = data.pokemon_count
			data.pokemonSpawnAvg = data.pokemon_avg

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${logReference}: [matched] Nest ${data.name} and ${whoCares.length} humans cared.`)
			} else {
				return []
			}

			data.shinyPossible = this.shinyPossible.isShinyPossible(data.pokemonId, data.formId)

			// Icon URLs are pre-computed by the processor enrichment

			const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })
			const jobs = []

			// Attempt to calculate best position for nest
			const position = this.tileserverPregen.autoposition({
				polygons:
					JSON.parse(data.poly_path).map((x) => ({ path: x })),
			}, 500, 250)
			data.zoom = Math.min(position.zoom, 16)
			data.map_longitude = position.longitude
			data.map_latitude = position.latitude

			await this.getStaticMapUrl(logReference, data, 'nest', ['map_latitude', 'map_longitude', 'zoom', 'imgUrl', 'poly_path'])
			data.staticmap = data.staticMap // deprecated

			for (const cares of whoCares) {
				this.log.debug(`${logReference}: [matched] Creating nest alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`, cares)

				this.log.verbose(`${logReference}: [matched] Creating nest alert for ${cares.type} ${cares.id} ${cares.name} ${cares.language} ${cares.template}`)

				const language = cares.language || this.config.general.locale
				const translator = this.translatorFactory.Translator(language)
				let [platform] = cares.type.split(':')
				if (platform === 'webhook') platform = 'discord'

				// Pre-translated names from processor per-language enrichment
				const langEnrichment = this.getLanguageEnrichment(data, language)
				data.name = langEnrichment.name
				data.formName = langEnrichment.formName || ''
				data.fullName = langEnrichment.fullName
				data.typeName = langEnrichment.typeName || ''
				data.color = langEnrichment.color

				// Per-platform emoji lookups
				const typeEmojiKeys = data.typeEmojiKeys || []
				data.emoji = typeEmojiKeys.map((key) => translator.translate(this.emojiLookup.lookup(key, platform)))
				data.emojiString = data.emoji.join('')
				data.typeEmoji = data.emojiString
				data.shinyPossibleEmoji = data.shinyPossible ? translator.translate(this.emojiLookup.lookup('shiny', platform)) : ''

				const view = {
					...geoResult,
					...data,
					time: data.distime,
					tthd: data.tth.days,
					tthh: data.tth.hours,
					tthm: data.tth.minutes,
					tths: data.tth.seconds,
					now: new Date(),
					nowISO: new Date().toISOString(),
					areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
				}

				const templateType = 'nest'
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
			this.log.error(`${data.pokestop_id}: [matched] Can't seem to handle nest: `, e, data)
		}
	}
}

module.exports = Nest
