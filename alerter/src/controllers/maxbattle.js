const moment = require('moment-timezone')
require('moment-precise-range-plugin')

const Controller = require('./controller')
const nightTime = require('./common/nightTime')
const weatherCommon = require('./common/weather')
const evolutionCalculator = require('./common/evolutionCalculator')

class Maxbattle extends Controller {
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
			data.googleMapUrl = `https://maps.google.com/maps?q=${data.latitude},${data.longitude}`
			data.appleMapUrl = `https://maps.apple.com/place?coordinate=${data.latitude},${data.longitude}`
			data.wazeMapUrl = `https://www.waze.com/ul?ll=${data.latitude},${data.longitude}&navigate=yes&zoom=17`
			if (this.config.general.rdmURL) {
				data.rdmUrl = `${this.config.general.rdmURL}${!this.config.general.rdmURL.endsWith('/') ? '/' : ''}?lat=${data.latitude}&lon=${data.longitude}&zoom=18.0`
			}
			if (this.config.general.reactMapURL) {
				data.reactMapUrl = `${this.config.general.reactMapURL}${!this.config.general.reactMapURL.endsWith('/') ? '/' : ''}id/stations/${data.stationId}/16`
			}
			if (this.config.general.rocketMadURL) {
				data.rocketMadUrl = `${this.config.general.rocketMadURL}${!this.config.general.rocketMadURL.endsWith('/') ? '/' : ''}?lat=${data.latitude}&lon=${data.longitude}&zoom=18.0`
			}

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
			data.gameWeatherNameEng = data.weather ? this.GameData.utilData.weather[data.gameWeatherId].name : ''

			data.levelNameEng = this.GameData.utilData.maxbattleLevels ? this.GameData.utilData.maxbattleLevels[data.level] : `Level ${data.level}`

			if (!data.pokemonId) return []

			data.form ??= 0
			const monster = this.GameData.monsters[`${data.pokemonId}_${data.form}`] || this.GameData.monsters[`${data.pokemonId}_0`]
			if (!monster) {
				this.log.warn(`${logReference}: Couldn't find monster in:`, data)
				return []
			}

			data.nameEng = monster.name
			data.formId = monster.form.id
			data.formNameEng = monster.form.name
			data.genderDataEng = this.GameData.utilData.genders[data.gender]
			data.evolutionNameEng = data.evolution ? this.GameData.utilData.evolution[data.evolution].name : ''
			if (!data.tth) data.tth = moment.preciseDiff(Date.now(), data.battle_end * 1000, true)
			data.quickMoveId = data.move_1 ?? ''
			data.chargeMoveId = data.move_2 ?? ''
			data.quickMoveNameEng = this.GameData.moves[data.move_1] ? this.GameData.moves[data.move_1].name : ''
			data.chargeMoveNameEng = this.GameData.moves[data.move_2] ? this.GameData.moves[data.move_2].name : ''
			data.shinyPossible = this.shinyPossible.isShinyPossible(data.pokemonId, data.formId)
			data.generation = this.GameData.utilData.genException[`${data.pokemonId}_${data.form}`] || Object.entries(this.GameData.utilData.genData)
				.find(([, genData]) => data.pokemonId >= genData.min && data.pokemonId <= genData.max)?.[0]
			data.generationNameEng = this.GameData.utilData.genData[data.generation]?.name
			data.generationRoman = this.GameData.utilData.genData[data.generation]?.roman

			if (data.tth.firstDateWasLater || ((data.tth.hours * 3600) + (data.tth.minutes * 60) + data.tth.seconds) < minTth) {
				this.log.debug(`${logReference}: MaxBattle on ${data.stationName} already disappeared or is about to expire`)
				return []
			}

			const whoCares = matchedUsers
			if (!whoCares || !whoCares.length) return []

			this.log.info(`${logReference}: Maxbattle level ${data.level} on ${data.stationName} at [${data.latitude.toFixed(3)},${data.longitude.toFixed(3)}] and ${whoCares.length} humans cared.`)

			let discordCacheBad = true
			whoCares.forEach((cares) => {
				if (!this.isRateLimited(cares.id)) discordCacheBad = false
			})

			if (discordCacheBad) {
				whoCares.forEach((cares) => {
					this.log.verbose(`${logReference}: Not creating maxbattle alert (Rate limit) for ${cares.type} ${cares.id} ${cares.name}`)
				})
				return []
			}

			try {
				if (this.imgUicons) data.imgUrl = await this.imgUicons.pokemonIcon(data.pokemonId, data.form, data.evolution, data.gender, data.costume, data.alignment || 0, data.shinyPossible && this.config.general.requestShinyImages, data.bread) || this.config.fallbacks?.imgUrlStation
				if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.pokemonIcon(data.pokemonId, data.form, data.evolution, data.gender, data.costume, data.alignment || 0, data.shinyPossible && this.config.general.requestShinyImages, data.bread) || this.config.fallbacks?.imgUrlStation
				if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.pokemonIcon(data.pokemonId, data.form, data.evolution, data.gender, data.costume, data.alignment || 0, data.shinyPossible && this.config.general.requestShinyImages, data.bread)

				const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })
				const jobs = []

				nightTime.setNightTime(data, this.config)
				await this.getStaticMapUrl(logReference, data, 'maxbattle', ['battle_pokemon_id', 'latitude', 'longitude', 'battle_pokemon_form', 'battle_level', 'imgUrl', 'style'])
				data.intersection = await this.obtainIntersection(data)
				data.staticmap = data.staticMap // deprecated
				data.types = monster.types.map((type) => type.id)

				await weatherCommon.calculateForecastImpact(data, this.GameData, null, this.weatherData, data.battle_end, this.config)

				for (const cares of whoCares) {
					const rateLimitTtr = this.getRateLimitTimeToRelease(cares.id)
					if (rateLimitTtr) {
						this.log.verbose(`${logReference}: Not creating maxbattle alert (Rate limit) for ${cares.type} ${cares.id} ${cares.name} TTR: ${rateLimitTtr}`)
					} else {
						const language = cares.language || this.config.general.locale
						const translator = this.translatorFactory.Translator(language)
						let [platform] = cares.type.split(':')
						if (platform === 'webhook') platform = 'discord'

						data.name = translator.translate(data.nameEng)
						data.formName = translator.translate(data.formNameEng)
						data.evolutionName = translator.translate(data.evolutionNameEng)
						data.formNormalisedEng = data.formNameEng === 'Normal' ? '' : data.formNameEng
						data.formNormalised = translator.translate(data.formNormalisedEng)

						if (data.evolution) {
							data.fullNameEng = translator.format(this.GameData.utilData.megaName[data.evolution], data.nameEng.concat(data.formNormalisedEng ? ' ' : '', data.formNormalisedEng))
							data.fullName = translator.translateFormat(this.GameData.utilData.megaName[data.evolution], data.name.concat(data.formNormalised ? ' ' : '', data.formNormalised))
						} else {
							data.fullNameEng = data.nameEng.concat(data.formNormalisedEng ? ' ' : '', data.formNormalisedEng)
							data.fullName = data.name.concat(data.formNormalised ? ' ' : '', data.formNormalised)
						}

						data.levelName = translator.translateFormat(data.levelNameEng)
						data.megaName = data.evolution ? translator.translateFormat(this.GameData.utilData.megaName[data.evolution], data.name) : data.name
						data.quickMoveName = this.GameData.moves[data.move_1] ? translator.translate(this.GameData.moves[data.move_1].name) : ''
						data.quickMoveEmoji = this.GameData.moves[data.move_1] && this.GameData.moves[data.move_1].type ? translator.translate(this.emojiLookup.lookup(this.GameData.utilData.types[this.GameData.moves[data.move_1].type].emoji, platform)) : ''
						data.chargeMoveName = this.GameData.moves[data.move_2] ? translator.translate(this.GameData.moves[data.move_2].name) : ''
						data.chargeMoveEmoji = this.GameData.moves[data.move_2] && this.GameData.moves[data.move_2].type ? translator.translate(this.emojiLookup.lookup(this.GameData.utilData.types[this.GameData.moves[data.move_2].type].emoji, platform)) : ''
						data.gameWeatherName = data.weather ? translator.translate(data.gameWeatherNameEng) : ''
						data.gameWeatherEmoji = data.weather ? translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[data.weather].emoji, platform)) : ''
						data.shinyPossibleEmoji = data.shinyPossible ? translator.translate(this.emojiLookup.lookup('shiny', platform)) : ''
						data.generationName = translator.translate(data.generationNameEng)

						const e = []
						const n = []
						monster.types.forEach((type) => {
							e.push(this.emojiLookup.lookup(this.GameData.utilData.types[type.name].emoji, platform))
							n.push(type.name)
						})
						data.typeNameEng = n
						data.emoji = e
						data.typeName = data.typeNameEng.map((type) => translator.translate(type)).join(', ')
						data.typeEmoji = data.emoji.map((emoji) => translator.translate(emoji)).join('')

						data.boostingWeathers = data.types.map((type) => parseInt(Object.keys(this.GameData.utilData.weatherTypeBoost).find((key) => this.GameData.utilData.weatherTypeBoost[key].includes(type)), 10))
						data.boostingWeathersEmoji = data.boostingWeathers.map((weather) => translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[weather].emoji, platform))).join('')
						data.boosted = !!data.boostingWeathers.includes(data.weather)
						data.boostWeatherNameEng = data.boosted ? this.GameData.utilData.weather[data.weather].name : ''
						data.boostWeatherId = data.boosted ? data.weather : ''
						data.boostWeatherName = data.boosted ? translator.translate(this.GameData.utilData.weather[data.weather].name) : ''
						data.boostWeatherEmoji = data.boosted ? translator.translate(this.emojiLookup.lookup(this.GameData.utilData.weather[data.weather].emoji, platform)) : ''

						evolutionCalculator.setEvolutions(data, this.GameData, this.log, logReference, translator, this.emojiLookup, platform, monster)
						weatherCommon.setNextWeatherText(data, translator, this.GameData, this.emojiLookup, platform)

						/* Weakness calculations */
						const typeInfo = this.GameData.types
						const typeData = this.GameData.utilData.types
						const weaknesses = {}

						for (const type of data.typeNameEng) {
							typeInfo[type].weaknesses.forEach((x) => {
								if (!weaknesses[x.typeName]) weaknesses[x.typeName] = 1
								weaknesses[x.typeName] *= 2
							})
							typeInfo[type].resistances.forEach((x) => {
								if (!weaknesses[x.typeName]) weaknesses[x.typeName] = 1
								weaknesses[x.typeName] *= 0.5
							})
							typeInfo[type].immunes.forEach((x) => {
								if (!weaknesses[x.typeName]) weaknesses[x.typeName] = 1
								weaknesses[x.typeName] *= 0.25
							})
						}

						const typeObj = {
							extraWeak: { value: 4, types: [], text: 'Very vulnerable to' },
							weak: { value: 2, types: [], text: 'Vulnerable to' },
							resist: { value: 0.5, types: [], text: 'Resistant to' },
							immune: { value: 0.25, types: [], text: 'Very resistant to' },
							extraImmune: { value: 0.125, types: [], text: 'Extremely resistant to' },
						}

						for (const [name, value] of Object.entries(weaknesses)) {
							const translated = {
								nameEng: name,
								name: translator.translate(name),
								emoji: translator.translate(this.emojiLookup.lookup(typeData[name].emoji, platform)),
							}
							switch (value) {
								case 0.125: typeObj.extraImmune.types.push(translated); break
								case 0.25: typeObj.immune.types.push(translated); break
								case 0.5: typeObj.resist.types.push(translated); break
								case 2: typeObj.weak.types.push(translated); break
								case 4: typeObj.extraWeak.types.push(translated); break
								default: break
							}
						}

						let weaknessEmoji = ''
						for (const info of Object.values(typeObj)) {
							if (info.types.length) {
								const typeEmoji = info.types.map((x) => x.emoji).join('')
								info.typeEmoji = typeEmoji
								weaknessEmoji = weaknessEmoji.concat(`${info.value}x${typeEmoji} `)
							}
						}

						data.weaknessList = Object.values(typeObj).filter((x) => x.types.length)
						data.weaknessEmoji = weaknessEmoji

						const view = {
							...geoResult,
							...data,
							id: data.pokemonId,
							baseStats: monster.stats,
							time: data.disappearTime,
							tthd: data.tth.days,
							tthh: data.tth.hours,
							tthm: data.tth.minutes,
							tths: data.tth.seconds,
							now: new Date(),
							nowISO: new Date().toISOString(),
							genderData: {
								name: translator.translate(data.genderDataEng.name),
								emoji: translator.translate(this.emojiLookup.lookup(data.genderDataEng.emoji, platform)),
							},
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
