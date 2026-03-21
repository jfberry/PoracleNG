const Controller = require('./controller')
const nightTime = require('./common/nightTime')

class Maxbattle extends Controller {
	/**
	 * Handle a pre-matched max battle payload from the Go processor.
	 * All game data lookups, translations, weakness calculations, generation,
	 * weather forecast impact, evolution chains, and map URLs are pre-computed
	 * by the processor.
	 *
	 * This controller handles:
	 * - Per-platform emoji lookups (using emoji keys from enrichment)
	 * - Icon URL fetching (uicons)
	 * - Geocoding + static map (external I/O)
	 * - Weather change text formatting (using pre-translated weather names)
	 * - Template rendering
	 */
	async handleMatched(obj, matchedUsers, matchedAreas) {
		const data = obj
		const minTth = this.config.general.alertMinimumTime || 0

		try {
			const logReference = data.id

			data.stationId = data.id
			data.pokemonId = data.battle_pokemon_id
			data.move_1 = data.battle_pokemon_move_1
			data.move_2 = data.battle_pokemon_move_2
			data.level = data.battle_level
			data.gmax = (data.level > 6) ? 1 : 0
			data.gender = data.battle_pokemon_gender
			data.evolution = 0
			data.form = data.battle_pokemon_form
			data.costume = data.battle_pokemon_costume
			data.alignment = data.battle_pokemon_alignment
			data.bread = data.battle_pokemon_bread_mode
			data.color = 'D000C0'

			Object.assign(data, this.config.general.dtsDictionary)

			if (data.name) {
				data.name = this.escapeJsonString(data.name)
				data.stationName = data.name
			}

			// disappearTime and tth are pre-computed by the Go processor

			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			// Use processor-provided weather data
			data.weather = data.gameWeatherId || 0
			data.gameWeatherId = data.weather

			if (!data.pokemonId) return []

			data.form ??= 0
			data.quickMoveId = data.move_1 ?? ''
			data.chargeMoveId = data.move_2 ?? ''
			data.formId = data.form
			data.shinyPossible = data.shinyPossible || false

			if (data.tth.firstDateWasLater || ((data.tth.hours * 3600) + (data.tth.minutes * 60) + data.tth.seconds) < minTth) {
				this.log.debug(`${logReference}: MaxBattle on ${data.stationName} already disappeared or is about to expire`)
				return []
			}

			const whoCares = matchedUsers
			if (!whoCares || !whoCares.length) return []

			this.log.info(`${logReference}: Maxbattle level ${data.level} on ${data.stationName} at [${data.latitude.toFixed(3)},${data.longitude.toFixed(3)}] and ${whoCares.length} humans cared.`)

			try {
				// Icon URLs are pre-computed by the processor enrichment

				const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })
				const jobs = []

				nightTime.setNightTime(data, this.config)
				await this.getStaticMapUrl(logReference, data, 'maxbattle', ['battle_pokemon_id', 'latitude', 'longitude', 'battle_pokemon_form', 'battle_level', 'imgUrl', 'style'])
				data.intersection = await this.obtainIntersection(data)
				data.staticmap = data.staticMap // deprecated

				for (const cares of whoCares) {
					const language = cares.language || this.config.general.locale
					const translator = this.translatorFactory.Translator(language)
					let [platform] = cares.type.split(':')
					if (platform === 'webhook') platform = 'discord'

					// Pre-translated names and game data from processor per-language enrichment
					const langEnrichment = this.getLanguageEnrichment(data, language)
					data.name = langEnrichment.name
					data.formName = langEnrichment.formName || ''
					data.formNormalised = langEnrichment.formNormalised || ''
					data.fullName = langEnrichment.fullName
					data.quickMoveName = langEnrichment.quickMoveName || ''
					data.chargeMoveName = langEnrichment.chargeMoveName || ''
					data.boosted = langEnrichment.boosted
					data.boostWeatherId = langEnrichment.boostWeatherId
					data.boostWeatherName = langEnrichment.boostWeatherName || ''
					data.generationName = langEnrichment.generationName || ''
					data.genderName = langEnrichment.genderName || ''
					data.gameWeatherName = langEnrichment.gameWeatherName || ''
					data.levelName = langEnrichment.levelName || ''
					data.typeName = langEnrichment.typeName || ''
					data.evolutionName = langEnrichment.evolutionName || ''
					data.megaName = langEnrichment.megaName || data.name
					if (langEnrichment.weaknessList) data.weaknessList = langEnrichment.weaknessList

					// Evolutions and mega evolutions (pre-computed by processor per language)
					data.evolutions = langEnrichment.evolutions || []
					data.hasEvolutions = data.evolutions.length > 0
					data.megaEvolutions = langEnrichment.megaEvolutions || []
					data.hasMegaEvolutions = data.megaEvolutions.length > 0

					// Per-platform emoji lookups (using emoji keys from enrichment)
					data.genderData = {
						name: data.genderName,
						emoji: langEnrichment.genderEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.genderEmojiKey, platform)) : '',
					}
					data.shinyPossibleEmoji = data.shinyPossible ? translator.translate(this.emojiLookup.lookup('shiny', platform)) : ''
					data.quickMoveEmoji = langEnrichment.quickMoveTypeEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.quickMoveTypeEmojiKey, platform)) : ''
					data.chargeMoveEmoji = langEnrichment.chargeMoveTypeEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.chargeMoveTypeEmojiKey, platform)) : ''
					data.boostWeatherEmoji = langEnrichment.boostWeatherEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.boostWeatherEmojiKey, platform)) : ''
					data.gameWeatherEmoji = langEnrichment.gameWeatherEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.gameWeatherEmojiKey, platform)) : ''

					// Type emoji array (resolve emoji keys per platform)
					const typeEmojiKeys = data.typeEmojiKeys || []
					data.emoji = typeEmojiKeys.map((key) => translator.translate(this.emojiLookup.lookup(key, platform)))
					data.emojiString = data.emoji.join('')
					data.typeEmoji = data.emojiString

					// Boosting weathers emoji (resolve keys per platform)
					const boostingWeatherEmojiKeys = data.boostingWeatherEmojiKeys || []
					data.boostingWeathersEmoji = boostingWeatherEmojiKeys.map((key) => translator.translate(this.emojiLookup.lookup(key, platform))).join('')

					// Evolution type emoji (resolve keys per platform)
					for (const evo of data.evolutions) {
						if (evo.typeEmojiKeys) {
							evo.typeEmoji = evo.typeEmojiKeys.map((key) => translator.translate(this.emojiLookup.lookup(key, platform))).join('')
						}
					}
					for (const mega of data.megaEvolutions) {
						if (mega.typeEmojiKeys) {
							mega.typeEmoji = mega.typeEmojiKeys.map((key) => translator.translate(this.emojiLookup.lookup(key, platform))).join('')
						}
					}

					// Weakness emoji (resolve keys per platform)
					if (data.weaknessList) {
						let weaknessEmoji = ''
						for (const info of data.weaknessList) {
							if (info.types && info.types.length) {
								const typeEmoji = info.types.map((t) => (t.emojiKey ? translator.translate(this.emojiLookup.lookup(t.emojiKey, platform)) : (t.emoji || ''))).join('')
								info.typeEmoji = typeEmoji
								weaknessEmoji = weaknessEmoji.concat(`${info.value}x${typeEmoji} `)
							}
						}
						data.weaknessEmoji = weaknessEmoji
					}

					// Weather change text (using pre-computed weatherCurrent/weatherNext from processor)
					if (data.weatherNext) {
						const weatherNextInfo = this.GameData.utilData.weather[data.weatherNext] || {}
						const weatherCurrentInfo = data.weatherCurrent ? (this.GameData.utilData.weather[data.weatherCurrent] || {}) : null
						if (!data.weatherCurrent) {
							data.weatherChange = `⚠️ ${translator.translate('Possible weather change at')} ${data.weatherChangeTime} : ➡️ ${translator.translate(weatherNextInfo.name)} ${translator.translate(this.emojiLookup.lookup(weatherNextInfo.emoji, platform))}`
							data.weatherCurrentName = translator.translate('unknown')
							data.weatherCurrentEmoji = '❓'
						} else {
							data.weatherChange = `⚠️ ${translator.translate('Possible weather change at')} ${data.weatherChangeTime} : ${translator.translate(weatherCurrentInfo.name)} ${translator.translate(this.emojiLookup.lookup(weatherCurrentInfo.emoji, platform))} ➡️ ${translator.translate(weatherNextInfo.name)} ${translator.translate(this.emojiLookup.lookup(weatherNextInfo.emoji, platform))}`
							data.weatherCurrentName = translator.translate(weatherCurrentInfo.name)
							data.weatherCurrentEmoji = translator.translate(this.emojiLookup.lookup(weatherCurrentInfo.emoji, platform))
						}
						data.weatherNextName = translator.translate(weatherNextInfo.name)
						data.weatherNextEmoji = translator.translate(this.emojiLookup.lookup(weatherNextInfo.emoji, platform))
					}

					const view = {
						...geoResult,
						...data,
						id: data.pokemonId,
						baseStats: data.baseStats || {},
						time: data.disappearTime,
						tthd: data.tth.days,
						tthh: data.tth.hours,
						tthm: data.tth.minutes,
						tths: data.tth.seconds,
						now: new Date(),
						nowISO: new Date().toISOString(),
						genderData: data.genderData,
						areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
					}

					const templateType = 'maxbattle'
					const message = await this.createMessage(logReference, templateType, platform, cares.template, language, cares.ping, view)

					jobs.push({
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
					})
				}
				return jobs
			} catch (e) {
				this.log.error(`${data.id}: Can't seem to handle maxbattle (user cared): `, e, data)
				return []
			}
		} catch (e) {
			this.log.error(`${data.id}: Can't seem to handle maxbattle`, e, data)
			return []
		}
	}
}

module.exports = Maxbattle
