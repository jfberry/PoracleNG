const Controller = require('./controller')
const nightTime = require('./common/nightTime')

class Raid extends Controller {
	/**
	 * Handle a pre-matched raid/egg payload from the Go processor.
	 * All game data lookups, translations, weakness calculations, generation,
	 * weather forecast impact, evolution chains, map URLs, and move names
	 * are pre-computed by the processor.
	 *
	 * This controller handles:
	 * - Per-platform emoji lookups (using emoji keys from enrichment)
	 * - Icon URL fetching (uicons)
	 * - Geocoding + static map (external I/O)
	 * - Night time calculation
	 * - Weather change text formatting (using pre-translated weather names)
	 * - Template rendering
	 */
	async handleMatched(obj, matchedUsers, matchedAreas, messageType) {
		const data = obj
		const minTth = this.config.general.alertMinimumTime || 0

		try {
			const logReference = data.gym_id

			Object.assign(data, this.config.general.dtsDictionary)

			data.team_id ??= 0
			if (data.name) {
				data.name = this.escapeJsonString(data.name)
				data.gymName = data.name
			}
			if (data.gym_name) {
				data.gym_name = this.escapeJsonString(data.gym_name)
				data.gymName = data.gym_name
			}
			data.gymId = data.gym_id
			data.teamId = data.team_id
			data.gymColor = data.gymColor || ''
			data.ex = !!(data.ex_raid_eligible ?? data.is_ex_raid_eligible)
			data.gymUrl = data.gym_url || data.url || ''
			// disappearTime is pre-computed by the Go processor
			data.applemap = data.appleMapUrl // deprecated
			data.mapurl = data.googleMapUrl // deprecated
			data.color = data.gymColor // deprecated
			data.distime = data.disappearTime // deprecated

			// Use processor-provided matched areas
			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			// Use processor-provided weather data
			data.weather = data.gameWeatherId || 0
			data.gameWeatherId = data.weather

			if (this.config.general.ignoreLongRaids && (data.end - data.start) > 47 * 60) {
				this.log.verbose(`${logReference}: [matched] Raid/Egg on ${data.gymName} will be longer than 47 minutes - ignored`)
				return []
			}

			const unixMsNow = new Date().getTime()

			if (data.rsvps) {
				const newRsvps = []
				for (const rsvp of data.rsvps) {
					if (rsvp.timeslot > unixMsNow) {
						rsvp.timeSlot = Math.ceil(rsvp.timeslot / 1000)
						rsvp.time = rsvp.time || ''
						rsvp.goingCount = rsvp.going_count || 0
						rsvp.maybeCount = rsvp.maybe_count || 0
						newRsvps.push(rsvp)
					}
				}
				data.rsvps = newRsvps
			}

			const whoCares = matchedUsers

			if (messageType === 'raid' && data.pokemon_id) {
				// Hatched raid
				data.form ??= 0
				data.pokemonId = data.pokemon_id
				data.formId = data.form
				// tth is pre-computed by the Go processor
				data.quickMoveId = data.move_1 ?? ''
				data.chargeMoveId = data.move_2 ?? ''
				data.shinyPossible = this.shinyPossible.isShinyPossible(data.pokemonId, data.formId)

				if (data.tth.firstDateWasLater || ((data.tth.hours * 3600) + (data.tth.minutes * 60) + data.tth.seconds) < minTth) {
					this.log.debug(`${logReference}: [matched] Raid on ${data.gymName} already disappeared`)
					return []
				}

				if (whoCares.length) {
					this.log.info(`${logReference}: [matched] Raid level ${data.level} on ${data.gymName} at [${data.latitude.toFixed(3)},${data.longitude.toFixed(3)}] and ${whoCares.length} humans cared.`)
				} else {
					return []
				}

				try {
					// Icon URLs are pre-computed by the processor enrichment

					const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })
					const jobs = []

					nightTime.setNightTime(data, this.config)
					await this.getStaticMapUrl(logReference, data, 'raid', ['pokemon_id', 'latitude', 'longitude', 'form', 'level', 'imgUrl', 'style'])
					data.intersection = await this.obtainIntersection(data)
					data.staticmap = data.staticMap // deprecated

					for (const cares of whoCares) {
						// RSVP filtering is handled by the processor

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
						data.nameEng = langEnrichment.nameEng || ''
						data.evolutionName = langEnrichment.evolutionName || ''
						data.quickMoveName = langEnrichment.quickMoveName || ''
						data.chargeMoveName = langEnrichment.chargeMoveName || ''
						data.levelName = langEnrichment.levelName || ''
						data.teamName = langEnrichment.teamName || ''
						data.teamNameEng = langEnrichment.teamNameEng || ''
						data.generationName = langEnrichment.generationName || ''
						data.generation = langEnrichment.generation || ''
						data.generationRoman = langEnrichment.generationRoman || ''
						data.gameWeatherName = langEnrichment.gameWeatherName || ''
						data.boosted = langEnrichment.boosted
						data.boostWeatherId = langEnrichment.boostWeatherId
						data.boostWeatherName = langEnrichment.boostWeatherName || ''
						data.typeName = langEnrichment.typeName || ''
						data.gymColor = langEnrichment.teamColor || data.gymColor || ''

						// Deprecated aliases
						data.formname = data.formName
						data.evolutionname = data.evolutionName
						data.quickMove = data.quickMoveName
						data.chargeMove = data.chargeMoveName
						data.move1 = data.quickMoveName
						data.move2 = data.chargeMoveName

						// Per-platform emoji lookups (using emoji keys from enrichment)
						data.teamEmoji = langEnrichment.teamEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.teamEmojiKey, platform)) : ''
						data.quickMoveEmoji = langEnrichment.quickMoveTypeEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.quickMoveTypeEmojiKey, platform)) : ''
						data.chargeMoveEmoji = langEnrichment.chargeMoveTypeEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.chargeMoveTypeEmojiKey, platform)) : ''
						data.gameWeatherEmoji = langEnrichment.gameWeatherEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.gameWeatherEmojiKey, platform)) : ''
						data.shinyPossibleEmoji = data.shinyPossible ? translator.translate(this.emojiLookup.lookup('shiny', platform)) : ''
						data.boostWeatherEmoji = langEnrichment.boostWeatherEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.boostWeatherEmojiKey, platform)) : ''
						data.move1emoji = data.quickMoveEmoji // deprecated
						data.move2emoji = data.chargeMoveEmoji // deprecated

						// Type emoji array (resolve emoji keys per platform)
						const typeEmojiKeys = data.typeEmojiKeys || []
						data.emoji = typeEmojiKeys.map((key) => translator.translate(this.emojiLookup.lookup(key, platform)))
						data.typeEmoji = data.emoji.join('')

						// Boosting weathers emoji (resolve per platform)
						const boostingWeatherIds = data.boostingWeatherIds || []
						data.boostingWeathers = boostingWeatherIds
						data.boostingWeathersEmoji = boostingWeatherIds.map((weather) => (this.GameData.utilData.weather[weather] ? translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[weather].emoji, platform)) : '')).join('')

						// Gender data
						data.genderData = {
							name: langEnrichment.genderName || '',
							emoji: langEnrichment.genderEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.genderEmojiKey, platform)) : '',
						}

						// Mega name
						data.megaName = data.evolution ? langEnrichment.fullName : data.name

						// Evolutions and mega evolutions (pre-computed by processor per language)
						data.evolutions = langEnrichment.evolutions || []
						data.hasEvolutions = data.evolutions.length > 0
						data.megaEvolutions = langEnrichment.megaEvolutions || []
						data.hasMegaEvolutions = data.megaEvolutions.length > 0

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

						// Weakness list (pre-computed by processor per language)
						if (langEnrichment.weaknessList) {
							data.weaknessList = langEnrichment.weaknessList
							let weaknessEmoji = ''
							for (const info of data.weaknessList) {
								if (info.types && info.types.length) {
									const typeEmoji = info.types.map((t) => {
										const typeInfo = this.GameData.utilData.types ? Object.values(this.GameData.utilData.types).find((ti) => ti.id === t.typeId) : null
										return typeInfo ? translator.translate(this.emojiLookup.lookup(typeInfo.emoji, platform)) : ''
									}).join('')
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
								data.weatherChange = `\u26A0\uFE0F ${translator.translate('Possible weather change at')} ${data.weatherChangeTime} : \u27A1\uFE0F ${translator.translate(weatherNextInfo.name)} ${translator.translate(this.emojiLookup.lookup(weatherNextInfo.emoji, platform))}`
								data.weatherCurrentName = translator.translate('unknown')
								data.weatherCurrentEmoji = '\u2753'
							} else {
								data.weatherChange = `\u26A0\uFE0F ${translator.translate('Possible weather change at')} ${data.weatherChangeTime} : ${translator.translate(weatherCurrentInfo.name)} ${translator.translate(this.emojiLookup.lookup(weatherCurrentInfo.emoji, platform))} \u27A1\uFE0F ${translator.translate(weatherNextInfo.name)} ${translator.translate(this.emojiLookup.lookup(weatherNextInfo.emoji, platform))}`
								data.weatherCurrentName = translator.translate(weatherCurrentInfo.name)
								data.weatherCurrentEmoji = translator.translate(this.emojiLookup.lookup(weatherCurrentInfo.emoji, platform))
							}
							data.weatherNextName = translator.translate(weatherNextInfo.name)
							data.weatherNextEmoji = translator.translate(this.emojiLookup.lookup(weatherNextInfo.emoji, platform))
						}

						const view = {
							...geoResult,
							...data,
							pokemonName: data.pokemonName,
							id: data.pokemon_id,
							baseStats: data.baseStats || { baseAttack: 0, baseDefense: 0, baseStamina: 0 },
							time: data.disappearTime,
							tthh: data.tth.hours,
							tthm: data.tth.minutes,
							tths: data.tth.seconds,
							confirmedTime: data.disappear_time_verified,
							now: new Date(),
							nowISO: new Date().toISOString(),
							genderData: data.genderData,
							areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
						}

						const templateType = 'raid'
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
					this.log.error(`${data.gym_id}: [matched] Can't seem to handle raid (user cared): `, e, data)
					return []
				}
			}

			// Egg handling
			// tth is pre-computed by the Go processor
			// hatchTime is pre-computed by the Go processor
			data.hatchtime = data.hatchTime // deprecated

			if (data.tth.firstDateWasLater || ((data.tth.hours * 3600) + (data.tth.minutes * 60) + data.tth.seconds) < minTth) {
				this.log.debug(`${logReference}: [matched] Egg at ${data.gymName} already disappeared`)
				return []
			}

			if (whoCares.length) {
				this.log.info(`${logReference}: [matched] Egg level ${data.level} on ${data.gymName} at [${data.latitude.toFixed(3)},${data.longitude.toFixed(3)}] and ${whoCares.length} humans cared.`)
			} else {
				return []
			}

			try {
				// Icon URLs are pre-computed by the processor enrichment

				const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })
				const jobs = []

				nightTime.setNightTime(data, this.config)
				await this.getStaticMapUrl(logReference, data, 'raid', ['latitude', 'longitude', 'level', 'imgUrl'])
				data.staticmap = data.staticMap // deprecated

				for (const cares of whoCares) {
					// RSVP filtering is handled by the processor

					const language = cares.language || this.config.general.locale
					const translator = this.translatorFactory.Translator(language)
					let [platform] = cares.type.split(':')
					if (platform === 'webhook') platform = 'discord'

					// Pre-translated names from processor per-language enrichment
					const langEnrichment = this.getLanguageEnrichment(data, language)
					data.teamName = langEnrichment.teamName || ''
					data.teamNameEng = langEnrichment.teamNameEng || ''
					data.levelName = langEnrichment.levelName || ''
					data.gameWeatherName = langEnrichment.gameWeatherName || ''

					// Per-platform emoji lookups (using emoji keys from enrichment)
					data.teamEmoji = langEnrichment.teamEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.teamEmojiKey, platform)) : ''
					data.gameWeatherEmoji = langEnrichment.gameWeatherEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.gameWeatherEmojiKey, platform)) : ''

					const view = {
						...geoResult,
						...data,
						id: data.pokemon_id,
						time: data.hatchtime,
						tthh: data.tth.hours,
						tthm: data.tth.minutes,
						tths: data.tth.seconds,
						confirmedTime: data.disappear_time_verified,
						now: new Date(),
						nowISO: new Date().toISOString(),
						areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
					}

					const templateType = 'egg'
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
				this.log.error(`${data.gym_id}: [matched] Can't seem to handle egg (user cared): `, e, data)
				return []
			}
		} catch (e) {
			this.log.error(`${data.gym_id}: [matched] Can't seem to handle raid: `, e, data)
		}
	}
}

module.exports = Raid
