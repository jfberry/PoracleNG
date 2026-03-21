const Controller = require('./controller')

class Monster extends Controller {
	/**
	 * Handle a pre-matched pokemon payload from the Go processor.
	 * All game data lookups, translations, weakness calculations, generation,
	 * weather forecast impact, evolution chains, IV color, map URLs, and
	 * best PVP ranks are pre-computed by the processor.
	 *
	 * This controller handles:
	 * - Per-platform emoji lookups (using emoji keys from enrichment)
	 * - PVP display list building (per-user filter matching)
	 * - Icon URL fetching (uicons)
	 * - Geocoding + static map (external I/O)
	 * - Weather change text formatting (using pre-translated weather names)
	 * - Template rendering
	 */
	async handleMatched(obj, matchedUsers, matchedAreas) {
		const data = obj
		try {
			const logReference = data.encounter_id
			const minTth = this.config.general.alertMinimumTime || 0

			// All these fields are pre-computed by the Go processor enrichment:
			// data.encountered, data.iv, data.atk, data.def, data.sta, data.cp, data.level
			// data.catchBase, data.catchGreat, data.catchUltra
			// data.types, data.color, data.ivColor, data.generation, data.generationRoman
			// data.gameWeatherId, data.alteringWeathers, data.weaknessList
			// data.seenType, data.cell_coords
			// data.googleMapUrl, data.appleMapUrl, data.wazeMapUrl, data.rdmUrl, data.reactMapUrl, data.rocketMadUrl
			// data.disappearTime, data.tth, data.weatherChangeTime
			// data.weatherCurrent, data.weatherNext (forecast impact)
			// data.pvpBestRank, data.pvpEvolutionData
			// data.bestGreatLeagueRank, data.bestUltraLeagueRank, data.bestLittleLeagueRank
			// data.shinyPossible, data.shinyStats
			// data.hasEvolutions, data.hasMegaEvolutions
			// data.baseStats

			data.form ??= 0
			const { encountered } = data

			// Field aliases for backward compat
			if (data.pokestop_name) data.pokestop_name = this.escapeJsonString(data.pokestop_name)
			data.pokestopName = data.pokestop_name
			data.pokemonId = data.pokemon_id
			data.encounterId = data.encounter_id
			data.formId = data.form
			data.quickMoveId = data.quickMoveId ?? (encountered ? data.move_1 : 0)
			data.chargeMoveId = data.chargeMoveId ?? (encountered ? data.move_2 : 0)
			data.size = data.size ?? 0
			data.height = encountered && data.height ? (+data.height).toFixed(2) : 0
			data.weight = encountered && data.weight ? (+data.weight).toFixed(2) : 0
			if (data.boosted_weather) data.weather = data.boosted_weather
			if (!data.weather) data.weather = 0
			Object.assign(data, this.config.general.dtsDictionary)
			if (data.tthSeconds === undefined) data.tthSeconds = data.disappear_time - Date.now() / 1000
			data.confirmedTime = data.disappear_time_verified || data.verified
			data.pvpPokemonId = data.pokemon_id
			data.pvpFormId = data.form
			if (!data.pvpEvolutionData) data.pvpEvolutionData = {}
			data.shinyStats = data.shinyStats ? Math.round(data.shinyStats) : null

			// Deprecated aliases
			data.distime = data.disappearTime
			data.individual_attack = data.atk
			data.individual_defense = data.def
			data.individual_stamina = data.sta
			data.pokemon_level = data.level
			data.move_1 = data.quickMoveId
			data.move_2 = data.chargeMoveId
			data.applemap = data.appleMapUrl
			data.mapurl = data.googleMapUrl
			data.ivcolor = data.ivColor

			// Stop handling if it already disappeared or is about to go away
			if ((data.tth.firstDateWasLater || data.tthSeconds < minTth)) {
				this.log.verbose(`${data.encounter_id}: [matched] pokemon ${data.pokemon_id} already disappeared or is about to go away`)
				return []
			}

			// Use processor-provided matched areas
			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${data.encounter_id}: [matched] pokemon ${data.pokemon_id}{${encountered ? `${data.cp}/${data.iv}` : '?'}} at [${data.latitude.toFixed(3)},${data.longitude.toFixed(3)}] areas (${data.matched}) and ${whoCares.length} humans cared.`)
			} else {
				this.log.verbose(`${data.encounter_id}: [matched] pokemon ${data.pokemon_id} at [${data.latitude.toFixed(3)},${data.longitude.toFixed(3)}] with 0 humans`)
				return []
			}

			try {
				// Icon URLs are pre-computed by the processor enrichment (imgUrl, imgUrlAlt, stickerUrl)

				const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })
				const jobs = []

				data.intersection = await this.obtainIntersection(data)

				await this.getStaticMapUrl(
					logReference,
					data,
					'monster',
					['pokemon_id', 'latitude', 'longitude', 'form', 'costume', 'imgUrl', 'imgUrlAlt', 'style'],
					['pokemon_id', 'display_pokemon_id', 'latitude', 'longitude', 'verified', 'costume', 'form', 'pokemonId', 'generation', 'weather', 'confirmedTime', 'shinyPossible', 'seenType', 'seen_type', 'cell_coords', 'imgUrl', 'imgUrlAlt', 'nightTime', 'duskTime', 'dawnTime', 'style'],
				)
				data.staticmap = data.staticMap // deprecated

				if (this.config.general.populatePokestopName && !data.pokestopName && data.pokestop_id && this.scannerQuery) {
					data.pokestopName = this.escapeJsonString(await this.scannerQuery.getPokestopName(data.pokestop_id))
				}

				// Deduplicate matched users by ID (processor consolidates PVP filters)
				const seen = new Set()
				const uniqueUsers = whoCares.filter((u) => {
					if (seen.has(u.id)) return false
					seen.add(u.id)
					return true
				})

				for (const cares of uniqueUsers) {
					this.log.debug(`${logReference}: [matched] Creating monster alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`)

					const language = cares.language || this.config.general.locale
					const translator = this.translatorFactory.Translator(language)
					let [platform] = cares.type.split(':')
					if (platform === 'webhook') platform = 'discord'

					// Merge per-language enrichment (translated names) + per-user enrichment (PVP, distance)
					const langE = this.getLanguageEnrichment(data, language)
					const userE = data.perUserEnrichment?.[cares.id] || {}
					Object.assign(data, langE, userE)

					// Per-platform emoji lookups (only thing that varies by platform)
					data.genderData = {
						name: data.genderName || '',
						emoji: data.genderEmojiKey ? translator.translate(this.emojiLookup.lookup(data.genderEmojiKey, platform)) : '',
					}
					data.genderEmoji = data.genderData.emoji
					data.shinyPossibleEmoji = data.shinyPossible ? translator.translate(this.emojiLookup.lookup('shiny', platform)) : ''
					data.quickMoveEmoji = data.quickMoveTypeEmojiKey ? translator.translate(this.emojiLookup.lookup(data.quickMoveTypeEmojiKey, platform)) : ''
					data.chargeMoveEmoji = data.chargeMoveTypeEmojiKey ? translator.translate(this.emojiLookup.lookup(data.chargeMoveTypeEmojiKey, platform)) : ''
					data.boostWeatherEmoji = data.boostWeatherEmojiKey ? translator.translate(this.emojiLookup.lookup(data.boostWeatherEmojiKey, platform)) : ''
					data.gameWeatherEmoji = data.gameWeatherEmojiKey ? translator.translate(this.emojiLookup.lookup(data.gameWeatherEmojiKey, platform)) : ''
					data.bearingEmoji = data.bearingEmojiKey ? this.emojiLookup.lookup(data.bearingEmojiKey, platform) : ''

					// Type emoji array
					const typeEmojiKeys = data.typeEmojiKeys || []
					data.emoji = typeEmojiKeys.map((key) => translator.translate(this.emojiLookup.lookup(key, platform)))
					data.emojiString = data.emoji.join('')
					data.typeEmoji = data.emojiString

					// Evolution type emoji
					if (data.evolutions) {
						for (const evo of data.evolutions) {
							if (evo.typeEmojiKeys) {
								evo.typeEmoji = evo.typeEmojiKeys.map((key) => translator.translate(this.emojiLookup.lookup(key, platform))).join('')
							}
						}
					}
					if (data.megaEvolutions) {
						for (const mega of data.megaEvolutions) {
							if (mega.typeEmojiKeys) {
								mega.typeEmoji = mega.typeEmojiKeys.map((key) => translator.translate(this.emojiLookup.lookup(key, platform))).join('')
							}
						}
					}

					// Weakness emoji
					if (data.weaknessList) {
						for (const cat of data.weaknessList) {
							if (cat.types) {
								const typeEmoji = cat.types.map((t) => {
									const emojiKey = t.emojiKey || (this.GameData.utilData.types ? Object.values(this.GameData.utilData.types).find((ti) => ti.id === t.typeId)?.emoji : null)
									return emojiKey ? translator.translate(this.emojiLookup.lookup(emojiKey, platform)) : ''
								}).join('')
								cat.typeEmoji = typeEmoji
							}
						}
					}

					// Weather change text
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

					// Deprecated aliases
					data.formname = data.formName
					data.quickMove = data.quickMoveName
					data.chargeMove = data.chargeMoveName
					data.move1emoji = data.quickMoveEmoji
					data.move2emoji = data.chargeMoveEmoji
					data.boost = data.boostWeatherName
					data.boostemoji = data.boostWeatherEmoji
					data.gameweather = data.gameWeatherName
					data.gameweatheremoji = data.gameWeatherEmoji

					const view = {
						...geoResult,
						...data,
						id: data.pokemon_id,
						baseStats: data.baseStats,
						time: data.disappearTime,
						tthh: data.tth.hours,
						tthm: data.tth.minutes,
						tths: data.tth.seconds,
						now: new Date(),
						nowISO: new Date().toISOString(),
						areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
					}

					const templateType = (data.iv === -1) ? 'monsterNoIv' : 'monster'
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
				this.log.error(`${data.encounter_id}: [matched] Can't seem to handle monster (user cared): `, e, data)
				return []
			}
		} catch (e) {
			this.log.error(`${data.encounter_id}: [matched] Can't seem to handle monster: `, e, data)
		}
	}
}

module.exports = Monster
