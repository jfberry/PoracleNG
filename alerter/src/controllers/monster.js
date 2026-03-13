const Controller = require('./controller')

class Monster extends Controller {
	getAlteringWeathers(types, boostStatus) {
		const boostingWeathers = types.map((type) => parseInt(Object.keys(this.GameData.utilData.weatherTypeBoost).find((key) => this.GameData.utilData.weatherTypeBoost[key].includes(type)), 10))
		const nonBoostingWeathers = [1, 2, 3, 4, 5, 6, 7].filter((weather) => !boostingWeathers.includes(weather))
		if (boostStatus > 0) return nonBoostingWeathers
		return boostingWeathers
	}

	/**
	 * Handle a pre-matched pokemon payload from the Go processor.
	 * Skips whoCares — uses the provided matched users directly.
	 * Performs data enrichment, template rendering, and message creation.
	 */
	async handleMatched(obj, matchedUsers, matchedAreas) {
		const data = obj
		try {
			const logReference = data.encounter_id

			const minTth = this.config.general.alertMinimumTime || 0

			data.form ??= 0
			const monster = this.GameData.monsters[`${data.pokemon_id}_${data.form}`] || this.GameData.monsters[`${data.pokemon_id}_0`]

			if (!monster) {
				this.log.warn(`${data.encounter_id}: [matched] Couldn't find monster in:`, data)
				return []
			}

			// Use processor-provided weather data
			const currentCellWeather = data.gameWeatherId || 0

			const encountered = data.encountered !== undefined ? data.encountered : !(!(['string', 'number'].includes(typeof data.individual_attack) && (+data.individual_attack + 1))
				|| !(['string', 'number'].includes(typeof data.individual_defense) && (+data.individual_defense + 1))
				|| !(['string', 'number'].includes(typeof data.individual_stamina) && (+data.individual_stamina + 1)))

			if (data.pokestop_name) data.pokestop_name = this.escapeJsonString(data.pokestop_name)
			data.pokestopName = data.pokestop_name
			data.pokemonId = data.pokemon_id
			data.encounterId = data.encounter_id
			// eslint-disable-next-line prefer-destructuring
			data.generation = this.GameData.utilData.genException[`${data.pokemon_id}_${data.form}`] || Object.entries(this.GameData.utilData.genData).find(([, genData]) => data.pokemonId >= genData.min && data.pokemonId <= genData.max)[0]
			data.generationNameEng = this.GameData.utilData.genData[data.generation].name
			data.generationRoman = this.GameData.utilData.genData[data.generation].roman
			data.nameEng = monster.name
			data.formNameEng = monster.form.name
			data.formId = data.form
			// Use processor-provided computed fields if available, else compute
			if (data.iv === undefined) data.iv = encountered ? ((data.individual_attack + data.individual_defense + data.individual_stamina) / 0.45).toFixed(2) : -1
			if (data.atk === undefined) data.atk = encountered ? data.individual_attack : 0
			if (data.def === undefined) data.def = encountered ? data.individual_defense : 0
			if (data.sta === undefined) data.sta = encountered ? data.individual_stamina : 0
			if (data.base_catch) data.capture_1 = data.base_catch
			if (data.great_catch) data.capture_2 = data.great_catch
			if (data.ultra_catch) data.capture_3 = data.ultra_catch
			if (data.verified) data.disappear_time_verified = data.verified
			data.catchBase = encountered ? (data.capture_1 * 100).toFixed(2) : 0
			data.catchGreat = encountered ? (data.capture_2 * 100).toFixed(2) : 0
			data.catchUltra = encountered ? (data.capture_3 * 100).toFixed(2) : 0
			if (data.cp === undefined) data.cp = encountered ? data.cp : 0
			if (data.level === undefined) data.level = encountered ? data.pokemon_level : 0
			data.quickMoveId = encountered ? data.move_1 : 0
			data.chargeMoveId = encountered ? data.move_2 : 0
			data.size = data.size ?? 0
			data.quickMoveNameEng = encountered && this.GameData.moves[data.quickMoveId] ? this.GameData.moves[data.quickMoveId].name : ''
			data.chargeMoveNameEng = encountered && this.GameData.moves[data.chargeMoveId] ? this.GameData.moves[data.chargeMoveId].name : ''
			data.height = encountered && data.height ? data.height.toFixed(2) : 0
			data.weight = encountered && data.weight ? data.weight.toFixed(2) : 0
			data.genderDataEng = this.GameData.utilData.genders[data.gender]
			data.genderNameEng = data.genderDataEng.name
			if (data.boosted_weather) data.weather = data.boosted_weather
			if (!data.weather) data.weather = 0
			Object.assign(data, this.config.general.dtsDictionary)
			data.appleMapUrl = `https://maps.apple.com/place?coordinate=${data.latitude},${data.longitude}`
			data.googleMapUrl = `https://maps.google.com/maps?q=${data.latitude},${data.longitude}`
			data.wazeMapUrl = `https://www.waze.com/ul?ll=${data.latitude},${data.longitude}&navigate=yes&zoom=17`
			if (this.config.general.rdmURL) {
				data.rdmUrl = `${this.config.general.rdmURL}${!this.config.general.rdmURL.endsWith('/') ? '/' : ''}@pokemon/${data.encounter_id}`
			}
			if (this.config.general.reactMapURL) {
				data.reactMapUrl = `${this.config.general.reactMapURL}${!this.config.general.reactMapURL.endsWith('/') ? '/' : ''}id/pokemon/${data.encounter_id}`
			}
			if (this.config.general.rocketMadURL) {
				data.rocketMadUrl = `${this.config.general.rocketMadURL}${!this.config.general.rocketMadURL.endsWith('/') ? '/' : ''}?lat=${data.latitude}&lon=${data.longitude}&zoom=18.0`
			}
			data.color = this.GameData.utilData.types[monster.types[0].name].color
			data.ivColor = this.findIvColor(data.iv)
			if (data.tthSeconds === undefined) data.tthSeconds = data.disappear_time - Date.now() / 1000
			// tth and disappearTime are pre-computed by the Go processor
			data.confirmedTime = data.disappear_time_verified
			data.distime = data.disappearTime // deprecated
			data.individual_attack = data.atk // deprecated
			data.individual_defense = data.def // deprecated
			data.individual_stamina = data.sta // deprecated
			data.pokemon_level = data.level // deprecated
			data.move_1 = data.quickMoveId // deprecated
			data.move_2 = data.chargeMoveId // deprecated
			data.applemap = data.appleMapUrl // deprecated
			data.mapurl = data.googleMapUrl // deprecated
			data.ivcolor = data.ivColor // deprecated
			data.types = monster.types.map((type) => type.id)
			data.alteringWeathers = this.getAlteringWeathers(data.types, data.weather)
			// Use processor-provided rarity group
			if (data.rarityGroup === undefined) data.rarityGroup = -1
			data.rarityNameEng = this.GameData.utilData.rarity[data.rarityGroup] ? this.GameData.utilData.rarity[data.rarityGroup] : ''
			data.sizeNameEng = data.size && this.GameData.utilData.size[data.size] ? this.GameData.utilData.size[data.size] : ''
			data.pvpPokemonId = data.pokemon_id
			data.pvpFormId = data.form
			// Use processor-provided PVP data
			if (!data.pvpEvolutionData) data.pvpEvolutionData = {}
			data.shinyPossible = this.shinyPossible.isShinyPossible(data.pokemonId, data.formId)
			data.shinyStats = this.statsData.shinyData[data.pokemonId] ? this.statsData.shinyData[data.pokemonId].ratio.toFixed(0) : null

			// PVP rank calculation from webhook PVP data (same as handle())
			if (data.pvp) {
				for (const league of Object.keys(data.pvp)) {
					data[`pvp_rankings_${league}_league`] = data.pvp[league].filter((x) => this.config.pvp.includeMegaEvolution || !x.evolution)
				}
			}

			if (!data.pvpBestRank) data.pvpBestRank = {}

			const capsConsidered = this.config.pvp.levelCaps ?? [50]

			const rankCalculator = (league, leagueData, minCp) => {
				const best = Object.fromEntries(capsConsidered.map((x) => [x, { rank: 4096, cp: 0 }]))
				if (leagueData) {
					for (const stats of leagueData) {
						const caps = []
						if (!stats.cap && !stats.capped) {
							caps.push(50)
						} else if (stats.capped) {
							caps.push(...capsConsidered.filter((x) => x >= stats.cap))
						} else {
							caps.push(stats.cap)
						}
						for (const cap of caps) {
							if (!capsConsidered.includes(cap)) continue // eslint-disable-line no-continue
							if (stats.rank && stats.rank < best[cap].rank) {
								best[cap].rank = stats.rank
								best[cap].cp = stats.cp || 0
							} else if (stats.rank && stats.cp && stats.rank === best[cap].rank && stats.cp > best[cap].cp) {
								best[cap].cp = stats.cp
							}
						}

						if (!stats.evolution && this.config.pvp.pvpEvolutionDirectTracking && stats.rank && stats.cp && stats.pokemon !== data.pokemon_id && stats.rank <= this.config.pvp.pvpFilterMaxRank && stats.cp >= minCp) {
							const newEvo = {
								rank: stats.rank,
								percentage: stats.percentage,
								pokemon: stats.pokemon,
								form: stats.form || 0,
								level: stats.level,
								cp: stats.cp,
								caps: stats.capped ? capsConsidered.filter((x) => x >= stats.cap) : (stats.cap && capsConsidered.includes(stats.cap) ? [stats.cap] : null), // eslint-disable-line no-nested-ternary
							}
							if (data.pvpEvolutionData[stats.pokemon] && data.pvpEvolutionData[stats.pokemon][league]) {
								data.pvpEvolutionData[stats.pokemon][league].push(newEvo)
							} else {
								const leagueStats = {}
								leagueStats[league] = [newEvo]
								data.pvpEvolutionData[stats.pokemon] = { ...data.pvpEvolutionData[stats.pokemon], ...leagueStats }
							}
						}
					}

					const bestRanks = []
					for (const [cap, details] of Object.entries(best)) {
						let found = false
						for (const bestRank of bestRanks) {
							if (bestRank.cp === details.cp && bestRank.rank === details.rank) {
								bestRank.caps.push(cap)
								found = true
								break
							}
						}
						if (!found) {
							bestRanks.push({ ...details, caps: [cap] })
						}
					}
					data.pvpBestRank[league] = bestRanks
				}

				return {
					rank: Math.min(...Object.values(best).map((x) => x.rank)),
					cp: Math.min(...Object.values(best).map((x) => x.cp)),
				}
			}

			const ultraPvp = rankCalculator(2500, data.pvp_rankings_ultra_league, this.config.pvp.pvpFilterUltraMinCP)
			data.bestUltraLeagueRank = ultraPvp.rank
			data.bestUltraLeagueRankCP = ultraPvp.cp

			const greatPvp = rankCalculator(1500, data.pvp_rankings_great_league, this.config.pvp.pvpFilterGreatMinCP)
			data.bestGreatLeagueRank = greatPvp.rank
			data.bestGreatLeagueRankCP = greatPvp.cp

			const littlePvp = rankCalculator(500, data.pvp_rankings_little_league, this.config.pvp.pvpFilterLittleMinCP)
			data.bestLittleLeagueRank = littlePvp.rank
			data.bestLittleLeagueRankCP = littlePvp.cp

			// Stop handling if it already disappeared or is about to go away
			if ((data.tth.firstDateWasLater || data.tthSeconds < minTth)) {
				this.log.verbose(`${data.encounter_id}: [matched] ${monster.name} already disappeared or is about to go away`)
				return []
			}

			// Use processor-provided matched areas
			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${data.encounter_id}: [matched] ${monster.name}{${encountered ? `${data.cp}/${data.iv}` : '?'}} at [${data.latitude.toFixed(3)},${data.longitude.toFixed(3)}] areas (${data.matched}) and ${whoCares.length} humans cared.`)
			} else {
				this.log.verbose(`${data.encounter_id}: [matched] ${monster.name} at [${data.latitude.toFixed(3)},${data.longitude.toFixed(3)}] with 0 humans`)
				return []
			}

			// From here, same as handle() post-whoCares: data enrichment + message creation
			try {
				let discordCacheBad = true
				whoCares.forEach((cares) => {
					if (!this.isRateLimited(cares.id)) discordCacheBad = false
				})

				if (discordCacheBad) {
					whoCares.forEach((cares) => {
						this.log.verbose(`${logReference}: [matched] Not creating monster alert (Rate limit) for ${cares.type} ${cares.id} ${cares.name}`)
					})
					return []
				}

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

				if (data.display_pokemon_id && data.display_pokemon_id !== data.pokemon_id) {
					data.display_form ??= 0
					const displayMonster = this.GameData.monsters[`${data.display_pokemon_id}_${data.display_form}`] || this.GameData.monsters[`${data.display_pokemon_id}_0`]
					if (displayMonster) {
						data.disguisePokemonNameEng = displayMonster.name
						if (displayMonster.form) data.disguideFormNameEng = displayMonster.form.name
					}
				}

				if (this.imgUicons) data.imgUrl = await this.imgUicons.pokemonIcon(data.pokemon_id, data.form, 0, data.gender, data.costume, 0, data.shinyPossible && this.config.general.requestShinyImages) || this.config.fallbacks?.imgUrl
				if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.pokemonIcon(data.pokemon_id, data.form, 0, data.gender, data.costume, 0, data.shinyPossible && this.config.general.requestShinyImages) || this.config.fallbacks?.imgUrl
				if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.pokemonIcon(data.pokemon_id, data.form, 0, data.gender, data.costume, 0, data.shinyPossible && this.config.general.requestShinyImages)

				const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })

				const jobs = []

				require('./common/nightTime').setNightTime(data, this.config)

				data.intersection = await this.obtainIntersection(data)

				if (data.seen_type) {
					switch (data.seen_type) {
						case 'nearby_stop': data.seenType = 'pokestop'; break
						case 'nearby_cell': data.seenType = 'cell'; break
						case 'lure':
						case 'lure_wild': data.seenType = 'lure'; break
						case 'lure_encounter':
						case 'encounter':
						case 'wild': data.seenType = data.seen_type; break
						default: break
					}
				} else if (data.pokestop_id === 'None' && data.spawnpoint_id === 'None') data.seenType = 'cell'
				else if (data.pokestop_id === 'None') data.seenType = encountered ? 'encounter' : 'wild'
				else data.seenType = 'pokestop'

				// cell_coords are pre-computed by the Go processor

				await this.getStaticMapUrl(
					logReference,
					data,
					'monster',
					['pokemon_id', 'latitude', 'longitude', 'form', 'costume', 'imgUrl', 'imgUrlAlt', 'style'],
					['pokemon_id', 'display_pokemon_id', 'latitude', 'longitude', 'verified', 'costume', 'form', 'pokemonId', 'generation', 'weather', 'confirmedTime', 'shinyPossible', 'seenType', 'seen_type', 'cell_coords', 'imgUrl', 'imgUrlAlt', 'nightTime', 'duskTime', 'dawnTime', 'style'],
				)
				data.staticmap = data.staticMap // deprecated

				// Weather forecast
				const { nextHourTimestamp } = this.weatherData.getWeatherTimes()
				if (this.config.weather.enableWeatherForecast && data.disappear_time > nextHourTimestamp) {
					const weatherCellId = this.weatherData.getWeatherCellId(data.latitude, data.longitude)
					const weatherForecast = await this.weatherData.getWeatherForecast(weatherCellId)

					let pokemonShouldBeBoosted = false
					let pokemonWillBeBoosted = false
					const currentBoostedTypes = weatherForecast.current ? this.GameData.utilData.weatherTypeBoost[weatherForecast.current] : []
					const forecastBoostedTypes = weatherForecast.next ? this.GameData.utilData.weatherTypeBoost[weatherForecast.next] : []
					if (weatherForecast.current > 0 && currentBoostedTypes.filter((boostedType) => data.types.includes(boostedType)).length > 0) pokemonShouldBeBoosted = true
					if (weatherForecast.next > 0 && ((data.weather > 0 && weatherForecast.next !== data.weather) || (weatherForecast.current > 0 && weatherForecast.next !== weatherForecast.current) || (pokemonShouldBeBoosted && data.weather === 0))) {
						const weatherChangeTime = data.weatherChangeTime || ''
						pokemonWillBeBoosted = forecastBoostedTypes.filter((boostedType) => data.types.includes(boostedType)).length > 0 ? 1 : 0
						if (data.weather > 0 && !pokemonWillBeBoosted || data.weather === 0 && pokemonWillBeBoosted) {
							weatherForecast.current = data.weather > 0 ? data.weather : weatherForecast.current
							if (pokemonShouldBeBoosted && data.weather === 0) {
								data.weatherCurrent = 0
							} else {
								data.weatherCurrent = weatherForecast.current
							}
							data.weatherChangeTime = weatherChangeTime
							data.weatherNext = weatherForecast.next
						}
					}
				}

				// Future event processing
				const event = this.eventParser.eventChangesSpawn(Math.floor(Date.now() / 1000), data.disappear_time, data.latitude, data.longitude)
				if (event) {
					data.futureEvent = true
					data.futureEventTime = event.time
					data.futureEventName = event.name
					data.futureEventTrigger = event.reason
				}

				// Lookup pokestop name if needed
				if (this.config.general.populatePokestopName && !data.pokestopName && data.pokestop_id && this.scannerQuery) {
					data.pokestopName = this.escapeJsonString(await this.scannerQuery.getPokestopName(data.pokestop_id))
				}

				for (const cares of consolidatedAlerts) {
					this.log.debug(`${logReference}: [matched] Creating monster alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`)

					const rateLimitTtr = this.getRateLimitTimeToRelease(cares.id)
					if (rateLimitTtr) {
						this.log.verbose(`${logReference}: [matched] Not creating monster alert (Rate limit) for ${cares.type} ${cares.id} ${cares.name} Time to release: ${rateLimitTtr}`)
						continue // eslint-disable-line no-continue
					}

					const language = cares.language || this.config.general.locale
					const translator = this.translatorFactory.Translator(language)
					let [platform] = cares.type.split(':')
					if (platform === 'webhook') platform = 'discord'

					data.name = translator.translate(monster.name)
					data.formName = translator.translate(monster.form.name)
					if (data.disguisePokemonNameEng) data.disguisePokemonName = translator.translate(data.disguisePokemonNameEng)
					if (data.disguiseFormNameEng) data.disguiseFormName = translator.translate(data.disguiseFormNameEng)

					data.formNormalisedEng = data.formNameEng === 'Normal' ? '' : data.formNameEng
					data.formNormalised = translator.translate(data.formNormalisedEng)

					data.fullNameEng = data.nameEng.concat(data.formNormalisedEng ? ' ' : '', data.formNormalisedEng)
					data.fullName = data.name.concat(data.formNormalised ? ' ' : '', data.formNormalised)

					data.genderData = {
						name: translator.translate(data.genderDataEng.name),
						emoji: translator.translate(this.emojiLookup.lookup(data.genderDataEng.emoji, platform)),
					}
					data.genderName = data.genderData.name
					data.genderEmoji = data.genderData.emoji
					data.shinyPossibleEmoji = data.shinyPossible ? translator.translate(this.emojiLookup.lookup('shiny', platform)) : ''
					data.rarityName = translator.translate(data.rarityNameEng)
					data.sizeName = translator.translate(data.sizeNameEng)
					data.quickMoveName = encountered && this.GameData.moves[data.quickMoveId] ? translator.translate(this.GameData.moves[data.quickMoveId].name) : ''
					data.quickMoveEmoji = this.GameData.moves[data.quickMoveId] && this.GameData.utilData.types[this.GameData.moves[data.quickMoveId].type] ? translator.translate(this.emojiLookup.lookup(this.GameData.utilData.types[this.GameData.moves[data.quickMoveId].type].emoji, platform)) : ''
					data.chargeMoveName = encountered && this.GameData.moves[data.chargeMoveId] ? translator.translate(this.GameData.moves[data.chargeMoveId].name) : ''
					data.chargeMoveEmoji = this.GameData.moves[data.chargeMoveId] && this.GameData.utilData.types[this.GameData.moves[data.chargeMoveId].type] ? translator.translate(this.emojiLookup.lookup(this.GameData.utilData.types[this.GameData.moves[data.chargeMoveId].type].emoji, platform)) : ''
					data.boosted = !!data.weather
					data.generationName = translator.translate(data.generationNameEng)
					data.boostWeatherId = data.weather ? data.weather : ''
					data.boostWeatherName = data.weather ? translator.translate(this.GameData.utilData.weather[data.weather].name) : ''
					data.boostWeatherEmoji = data.weather ? translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[data.weather].emoji, platform)) : ''
					data.gameWeatherId = this.GameData.utilData.weather[currentCellWeather] ? currentCellWeather : ''
					data.gameWeatherName = this.GameData.utilData.weather[currentCellWeather] ? translator.translate(this.GameData.utilData.weather[currentCellWeather].name) : ''
					data.gameWeatherEmoji = this.GameData.utilData.weather[currentCellWeather] ? translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[currentCellWeather].emoji, platform)) : ''
					data.formname = data.formName // deprecated
					data.quickMove = data.quickMoveName // deprecated
					data.chargeMove = data.chargeMoveName // deprecated
					data.move1emoji = data.quickMoveEmoji // deprecated
					data.move2emoji = data.chargeMoveEmoji // deprecated
					data.boost = data.boostWeatherName // deprecated
					data.boostemoji = data.boostWeatherEmoji // deprecated
					data.gameweather = data.gameWeatherName // deprecated
					data.gameweatheremoji = data.gameWeatherEmoji // deprecated

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

					const e = []
					const n = []
					monster.types.forEach((type) => {
						e.push(translator.translate(this.emojiLookup.lookup(this.GameData.utilData.types[type.name].emoji, platform)))
						n.push(type.name)
					})
					data.emoji = e
					data.typeNameEng = n
					data.typeName = data.typeNameEng.map((type) => translator.translate(type)).join(', ')
					data.emojiString = e.join('')
					data.typeEmoji = data.emojiString

					require('./common/evolutionCalculator').setEvolutions(data, this.GameData, this.log, logReference, translator, this.emojiLookup, platform, monster)

					const createPvpDisplay = (leagueCap, leagueData, maxRank, minCp) => {
						const displayList = []
						for (const rank of leagueData) {
							if (rank.rank <= maxRank && rank.cp >= minCp) {
								const displayRank = {}
								displayRank.rank = +rank.rank
								displayRank.formId = +rank.form || 0
								displayRank.evolution = rank.evolution
								displayRank.level = +rank.level
								displayRank.cap = rank.cap
								displayRank.capped = rank.capped
								displayRank.levelWithCap = displayRank.cap && !displayRank.capped ? `${displayRank.level}/${displayRank.cap}` : displayRank.level
								displayRank.cp = rank.cp
								displayRank.pokemonId = +rank.pokemon
								displayRank.percentage = rank.percentage <= 1 ? (rank.percentage * 100).toFixed(2) : rank.percentage.toFixed(2)

								let monsterName
								let formName
								let stats
								let mon = this.GameData.monsters[`${displayRank.pokemonId}_${displayRank.formId}`]
								if (!mon) {
									mon = this.GameData.monsters[`${displayRank.pokemonId}_0`]
									if (!mon) {
										monsterName = `${translator.translate('Unknown monster')} ${displayRank.pokemonId}`
										stats = { baseAttack: 0, baseDefense: 0, baseStamina: 0 }
									} else {
										monsterName = mon.name
										stats = monster.stats
									}
									formName = `${displayRank.formId}`
								} else {
									monsterName = mon.name
									formName = mon.form.name
									if (formName === undefined || formName === 'Normal') formName = ''
									stats = monster.stats
								}
								displayRank.baseStats = stats
								displayRank.nameEng = monsterName
								displayRank.formEng = formName
								displayRank.name = translator.translate(monsterName)
								displayRank.form = translator.translate(formName)
								if (displayRank.evolution) {
									displayRank.fullNameEng = translator.format(this.GameData.utilData.megaName[displayRank.evolution], displayRank.nameEng.concat(displayRank.formEng ? ' ' : '', displayRank.formEng))
									displayRank.fullName = translator.translateFormat(this.GameData.utilData.megaName[displayRank.evolution], displayRank.name.concat(displayRank.form ? ' ' : '', displayRank.form))
								} else {
									displayRank.fullNameEng = displayRank.nameEng.concat(displayRank.formEng ? ' ' : '', displayRank.formEng)
									displayRank.fullName = displayRank.name.concat(displayRank.form ? ' ' : '', displayRank.form)
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
						best.nameEng = Array.from(new Set(best.list.map((x) => x.fullNameEng))).join(', ')
						return best
					}

					data.pvpGreat = data.pvp_rankings_great_league ? createPvpDisplay(1500, data.pvp_rankings_great_league, this.config.pvp.pvpDisplayMaxRank, this.config.pvp.pvpDisplayGreatMinCP) : null
					data.pvpGreatBest = calculateBestInfo(data.pvpGreat)
					data.pvpUltra = data.pvp_rankings_ultra_league ? createPvpDisplay(2500, data.pvp_rankings_ultra_league, this.config.pvp.pvpDisplayMaxRank, this.config.pvp.pvpDisplayUltraMinCP) : null
					data.pvpUltraBest = calculateBestInfo(data.pvpUltra)
					data.pvpLittle = data.pvp_rankings_little_league ? createPvpDisplay(500, data.pvp_rankings_little_league, this.config.pvp.pvpDisplayMaxRank, this.config.pvp.pvpDisplayLittleMinCP) : null
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
						baseStats: monster.stats,
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
