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
				// Consolidate alerts per user (merge PVP filters)
				const consolidatedAlerts = []
				for (const alert of whoCares) {
					let existingAlert = consolidatedAlerts.find((x) => x.id === alert.id)
					if (!existingAlert) {
						existingAlert = { ...alert, filters: [] }
						consolidatedAlerts.push(existingAlert)
					}
					if (alert.pvp_ranking_worst < 4096) {
						existingAlert.filters.push({
							pvp_ranking_league: alert.pvp_ranking_league,
							pvp_ranking_worst: alert.pvp_ranking_worst,
							pvp_ranking_cap: alert.pvp_ranking_cap,
						})
					}
				}

				// External I/O (icons, geocoding, static maps)
				if (this.imgUicons) data.imgUrl = await this.imgUicons.pokemonIcon(data.pokemon_id, data.form, 0, data.gender, data.costume, 0, data.shinyPossible && this.config.general.requestShinyImages) || this.config.fallbacks?.imgUrl
				if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.pokemonIcon(data.pokemon_id, data.form, 0, data.gender, data.costume, 0, data.shinyPossible && this.config.general.requestShinyImages) || this.config.fallbacks?.imgUrl
				if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.pokemonIcon(data.pokemon_id, data.form, 0, data.gender, data.costume, 0, data.shinyPossible && this.config.general.requestShinyImages)

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

				// Lookup pokestop name if needed
				if (this.config.general.populatePokestopName && !data.pokestopName && data.pokestop_id && this.scannerQuery) {
					data.pokestopName = this.escapeJsonString(await this.scannerQuery.getPokestopName(data.pokestop_id))
				}

				for (const cares of consolidatedAlerts) {
					this.log.debug(`${logReference}: [matched] Creating monster alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`)

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
					data.rarityName = langEnrichment.rarityName || ''
					data.sizeName = langEnrichment.sizeName || ''
					data.gameWeatherName = langEnrichment.gameWeatherName || ''
					data.typeName = langEnrichment.typeName || ''
					if (langEnrichment.disguisePokemonName) data.disguisePokemonName = langEnrichment.disguisePokemonName
					if (langEnrichment.disguiseFormName) data.disguiseFormName = langEnrichment.disguiseFormName
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
					data.genderEmoji = data.genderData.emoji
					data.shinyPossibleEmoji = data.shinyPossible ? translator.translate(this.emojiLookup.lookup('shiny', platform)) : ''
					data.quickMoveEmoji = langEnrichment.quickMoveTypeEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.quickMoveTypeEmojiKey, platform)) : ''
					data.chargeMoveEmoji = langEnrichment.chargeMoveTypeEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.chargeMoveTypeEmojiKey, platform)) : ''
					data.boostWeatherEmoji = data.boostWeatherId ? translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[data.boostWeatherId]?.emoji || '', platform)) : ''
					data.gameWeatherEmoji = this.GameData.utilData.weather[data.gameWeatherId] ? translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[data.gameWeatherId].emoji, platform)) : ''

					// Type emoji array (resolve emoji keys per platform)
					const typeEmojiKeys = data.typeEmojiKeys || []
					data.emoji = typeEmojiKeys.map((key) => translator.translate(this.emojiLookup.lookup(key, platform)))
					data.emojiString = data.emoji.join('')
					data.typeEmoji = data.emojiString

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

					// Weather change text (using pre-computed weatherCurrent/weatherNext from processor)
					if (data.weatherNext) {
						if (!data.weatherCurrent) {
							data.weatherChange = `⚠️ ${translator.translate('Possible weather change at')} ${data.weatherChangeTime} : ➡️ ${translator.translate(this.GameData.utilData.weather[data.weatherNext].name)} ${translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[data.weatherNext].emoji, platform))}`
							data.weatherCurrentName = translator.translate('unknown')
							data.weatherCurrentEmoji = '❓'
						} else {
							data.weatherChange = `⚠️ ${translator.translate('Possible weather change at')} ${data.weatherChangeTime} : ${translator.translate(this.GameData.utilData.weather[data.weatherCurrent].name)} ${translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[data.weatherCurrent].emoji, platform))} ➡️ ${translator.translate(this.GameData.utilData.weather[data.weatherNext].name)} ${translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[data.weatherNext].emoji, platform))}`
							data.weatherCurrentName = translator.translate(this.GameData.utilData.weather[data.weatherCurrent].name)
							data.weatherCurrentEmoji = translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[data.weatherCurrent].emoji, platform))
						}
						data.weatherNextName = translator.translate(this.GameData.utilData.weather[data.weatherNext].name)
						data.weatherNextEmoji = translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[data.weatherNext].emoji, platform))
					}

					// PVP display list — names/stats/computed fields are pre-enriched by processor,
					// only per-user filter matching remains here.
					const createPvpDisplay = (leagueCap, leagueData, maxRank, minCp) => {
						const displayList = []
						for (const rank of leagueData) {
							if (rank.rank <= maxRank && rank.cp >= minCp) {
								const displayRank = {
									rank: +rank.rank,
									formId: +rank.form || 0,
									evolution: rank.evolution,
									level: +rank.level,
									cap: rank.cap,
									capped: rank.capped,
									levelWithCap: rank.levelWithCap ?? (+rank.cap && !rank.capped ? `${+rank.level}/${rank.cap}` : +rank.level),
									cp: rank.cp,
									pokemonId: +rank.pokemon,
									percentage: rank.percentageFormatted ?? (rank.percentage <= 1 ? (rank.percentage * 100).toFixed(2) : rank.percentage.toFixed(2)),
									// Pre-enriched by processor (names, stats)
									baseStats: rank.baseStats || { baseAttack: 0, baseDefense: 0, baseStamina: 0 },
									name: rank.name || `Pokemon ${rank.pokemon}`,
									fullName: rank.fullName || `Pokemon ${rank.pokemon}`,
									formName: rank.formName || '',
									formNormalised: rank.formNormalised || '',
								}

								displayRank.matchesUserTrack = false
								if (cares.filters.length) {
									displayRank.passesFilter = false
									for (const filter of cares.filters) {
										if ((filter.pvp_ranking_league === leagueCap || filter.pvp_ranking_league === 0)
											&& (filter.pvp_ranking_cap === 0 || filter.pvp_ranking_cap === displayRank.cap || displayRank.capped)
											&& (filter.pvp_ranking_worst >= displayRank.rank)) {
											displayRank.passesFilter = true
											displayRank.matchesUserTrack = true
										}
									}
								} else {
									displayRank.passesFilter = true
								}
								if (!this.config.pvp.filterByTrack || displayRank.passesFilter) {
									displayList.push(displayRank)
								}
							}
						}
						return displayList.length ? displayList : null
					}

					const calculateBestInfo = (ranks) => {
						if (!ranks) return null
						const best = { rank: 4096, list: [] }
						for (const result of ranks) {
							if (result.rank === best.rank) {
								best.list.push(result)
							} else if (result.rank < best.rank) {
								best.rank = result.rank
								best.list = [result]
							}
						}
						best.name = Array.from(new Set(best.list.map((x) => x.fullName))).join(', ')
						return best
					}

					// Use pre-enriched PVP data from processor (names/stats already resolved)
					const pvpGreatData = langEnrichment.pvpEnriched_great_league || data.pvp_rankings_great_league
					const pvpUltraData = langEnrichment.pvpEnriched_ultra_league || data.pvp_rankings_ultra_league
					const pvpLittleData = langEnrichment.pvpEnriched_little_league || data.pvp_rankings_little_league
					data.pvpGreat = pvpGreatData ? createPvpDisplay(1500, pvpGreatData, this.config.pvp.pvpDisplayMaxRank, this.config.pvp.pvpDisplayGreatMinCP) : null
					data.pvpGreatBest = calculateBestInfo(data.pvpGreat)
					data.pvpUltra = pvpUltraData ? createPvpDisplay(2500, pvpUltraData, this.config.pvp.pvpDisplayMaxRank, this.config.pvp.pvpDisplayUltraMinCP) : null
					data.pvpUltraBest = calculateBestInfo(data.pvpUltra)
					data.pvpLittle = pvpLittleData ? createPvpDisplay(500, pvpLittleData, this.config.pvp.pvpDisplayMaxRank, this.config.pvp.pvpDisplayLittleMinCP) : null
					data.pvpLittleBest = calculateBestInfo(data.pvpLittle)
					data.pvpAvailable = data.pvpGreat !== null || data.pvpUltra !== null || data.pvpLittle !== null
					data.userHasPvpTracks = !!cares.filters.length

					data.distance = cares.distance ?? ''
					data.bearing = cares.bearing ?? ''
					data.bearingEmoji = cares.cardinalDirection ? this.emojiLookup.lookup(cares.cardinalDirection, platform) : ''

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
						pvpUserRanking: cares.pvp_ranking_worst === 4096 ? 0 : cares.pvp_ranking_worst,
						areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
						pvpDisplayMaxRank: this.config.pvp.pvpDisplayMaxRank,
						pvpDisplayGreatMinCP: this.config.pvp.pvpDisplayGreatMinCP,
						pvpDisplayUltraMinCP: this.config.pvp.pvpDisplayUltraMinCP,
						pvpDisplayLittleMinCP: this.config.pvp.pvpDisplayLittleMinCP,
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
