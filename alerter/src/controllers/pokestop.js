const Controller = require('./controller')
const nightTime = require('./common/nightTime')

class Invasion extends Controller {
	async handleMatched(obj, matchedUsers, matchedAreas) {
		const data = obj
		const minTth = this.config.general.alertMinimumTime || 0

		try {
			const logReference = data.pokestop_id

			Object.assign(data, this.config.general.dtsDictionary)

			// Map URLs are pre-computed by the Go processor enrichment
			data.googleMapUrl = data.googleMapUrl || `https://maps.google.com/maps?q=${data.latitude},${data.longitude}`
			data.appleMapUrl = data.appleMapUrl || `https://maps.apple.com/place?coordinate=${data.latitude},${data.longitude}`
			data.wazeMapUrl = data.wazeMapUrl || `https://www.waze.com/ul?ll=${data.latitude},${data.longitude}&navigate=yes&zoom=17`
			if (this.config.general.rdmURL) {
				data.rdmUrl = data.rdmUrl || `${this.config.general.rdmURL}${!this.config.general.rdmURL.endsWith('/') ? '/' : ''}@pokestop/${data.pokestop_id}`
			}
			if (this.config.general.reactMapURL) {
				data.reactMapUrl = data.reactMapUrl || `${this.config.general.reactMapURL}${!this.config.general.reactMapURL.endsWith('/') ? '/' : ''}id/pokestops/${data.pokestop_id}`
			}
			if (this.config.general.rocketMadURL) {
				data.rocketMadUrl = data.rocketMadUrl || `${this.config.general.rocketMadURL}${!this.config.general.rocketMadURL.endsWith('/') ? '/' : ''}?lat=${data.latitude}&lon=${data.longitude}&zoom=18.0`
			}
			data.name = data.name ? this.escapeJsonString(data.name) : this.escapeJsonString(data.pokestop_name)
			data.pokestopName = data.name
			data.url = data.url || this.config.fallbacks?.pokestopUrl
			data.pokestopUrl = data.url

			const incidentExpiration = data.incident_expiration ?? data.incident_expire_timestamp
			data.incidentExpiration = incidentExpiration
			// tth and disappearTime are pre-computed by the Go processor
			data.applemap = data.appleMapUrl // deprecated
			data.mapurl = data.googleMapUrl // deprecated
			data.distime = data.disappearTime // deprecated
			data.displayTypeId = data.display_type ?? data.incident_display_type ?? 0

			// Stop handling if it already disappeared or is about to go away
			if (data.tth.firstDateWasLater || ((data.tth.hours * 3600) + (data.tth.minutes * 60) + data.tth.seconds) < minTth) {
				this.log.debug(`${data.pokestop_id} [matched] Invasion already disappeared or is about to go away in: ${data.tth.hours}:${data.tth.minutes}:${data.tth.seconds}`)
				return []
			}

			// Use processor-provided matched areas
			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			// gruntTypeId resolution — use processor enrichment if available, else resolve locally
			data.gruntTypeId = 0
			if (data.incident_grunt_type && (data.incident_grunt_type !== 352)) {
				data.gruntTypeId = data.incident_grunt_type
			} else if (data.grunt_type && (data.displayTypeId <= 6)) {
				data.gruntTypeId = data.grunt_type
			} else if (data.incident_grunt_type === 352) {
				data.grunt_type = 0
				data.displayTypeId = 8
			}

			// Defaults before per-language enrichment
			data.gender = data.gruntGender || 0
			data.gruntName = ''
			data.gruntTypeColor = 'BABABA'
			data.gruntRewards = ''
			data.gruntRewardsList = {}

			// Use processor-provided gruntType if available, else look up from GameData
			if (data.gruntTypeId) {
				data.gender = data.gruntGender || 0
				data.gruntName = 'Grunt'
				data.gruntType = data.gruntType || 'Mixed'
				data.gruntRewards = ''
				if (!data.gruntType || data.gruntType === 'Mixed') {
					if (data.gruntTypeId in this.GameData.grunts) {
						const gruntType = this.GameData.grunts[data.gruntTypeId]
						data.gruntType = gruntType.type
						data.gender = gruntType.gender
					}
				}
			}

			// Event invasions
			if (((data.grunt_type === 0) || !data.grunt_type) && (data.displayTypeId >= 7)) {
				data.gender = 0
				data.gruntName = data.displayTypeId && this.GameData.utilData.pokestopEvent[data.displayTypeId].name ? this.GameData.utilData.pokestopEvent[data.displayTypeId].name : ''
				data.gruntType = data.displayTypeId && this.GameData.utilData.pokestopEvent[data.displayTypeId].name ? this.GameData.utilData.pokestopEvent[data.displayTypeId].name.toLowerCase() : ''
				data.gruntRewards = ''
				data.gruntTypeColor = data.displayTypeId && this.GameData.utilData.pokestopEvent[data.displayTypeId].color ? this.GameData.utilData.pokestopEvent[data.displayTypeId].color : 'BABABA'
			}

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${logReference}: [matched] Invasion of type ${data.gruntType} at ${data.pokestopName} and ${whoCares.length} humans cared.`)
			} else {
				return []
			}

			try {
				if (((data.grunt_type === 0) || !data.grunt_type) && (data.displayTypeId >= 7)) {
					if (this.imgUicons) data.imgUrl = await this.imgUicons.pokestopIcon(data.lureTypeId, true, data.displayTypeId) || this.config.fallbacks?.imgUrlPokestop
					if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.pokestopIcon(data.lureTypeId, true, data.displayTypeId) || this.config.fallbacks?.imgUrlPokestop
					if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.pokestopIcon(data.lureTypeId, true, data.displayTypeId)
				} else {
					if (this.imgUicons) data.imgUrl = await this.imgUicons.invasionIcon(data.gruntTypeId) || this.config.fallbacks?.imgUrlPokestop
					if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.invasionIcon(data.gruntTypeId) || this.config.fallbacks?.imgUrlPokestop
					if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.invasionIcon(data.gruntTypeId)
				}

				const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })
				const jobs = []

				nightTime.setNightTime(data, this.config)

				data.intersection = await this.obtainIntersection(data)

				await this.getStaticMapUrl(logReference, data, 'pokestop', ['latitude', 'longitude', 'imgUrl', 'gruntTypeId', 'displayTypeId', 'style'])
				data.staticmap = data.staticMap // deprecated

				for (const cares of whoCares) {
					this.log.debug(`${logReference}: [matched] Creating invasion alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`, cares)

					this.log.verbose(`${logReference}: [matched] Creating invasion alert for ${cares.type} ${cares.id} ${cares.name} ${cares.language} ${cares.template}`)

					const language = cares.language || this.config.general.locale
					const translator = this.translatorFactory.Translator(language)
					let [platform] = cares.type.split(':')
					if (platform === 'webhook') platform = 'discord'

					// Per-language enrichment from processor
					const langEnrichment = this.getLanguageEnrichment(data, language)

					// Weather: pre-translated name and emoji key from processor
					data.gameWeatherName = langEnrichment.gameWeatherName || ''
					data.gameWeatherEmoji = langEnrichment.gameWeatherEmojiKey ? translator.translate(this.emojiLookup.lookup(langEnrichment.gameWeatherEmojiKey, platform)) : ''
					data.gameweather = data.gameWeatherName // deprecated
					data.gameweatheremoji = data.gameWeatherEmoji // deprecated

					data.gruntTypeEmoji = translator.translate(this.emojiLookup.lookup('grunt-unknown', platform))

					if (((data.grunt_type === 0) || !data.grunt_type) && (data.displayTypeId >= 7)) {
						data.gruntName = translator.translate(data.displayTypeId && this.GameData.utilData.pokestopEvent[data.displayTypeId].name ? this.GameData.utilData.pokestopEvent[data.displayTypeId].name : '')
						data.gruntTypeEmoji = translator.translate(this.emojiLookup.lookup(this.GameData.utilData.pokestopEvent[data.displayTypeId].emoji, platform))
					}

					// Full grunt build with rewards/lineup
					if (data.gruntTypeId) {
						// Use processor-provided translated grunt name + type name
						data.gruntName = langEnrichment.gruntName || translator.translate('Grunt')
						data.gruntType = langEnrichment.gruntTypeName || translator.translate('Mixed')
						data.gender = data.gruntGender || 0
						data.gruntRewards = ''

						if (data.gruntTypeId in this.GameData.grunts) {
							const gruntType = this.GameData.grunts[data.gruntTypeId]
							const type = gruntType.type === 'Metal' ? 'Steel' : gruntType.type

							data.gender = gruntType.gender
							data.genderDataEng = this.GameData.utilData.genders[data.gender]
							if (!data.genderDataEng) {
								data.genderDataEng = { name: '', emoji: '' }
							}
							if (this.GameData.utilData.types[type]) {
								data.gruntTypeEmoji = translator.translate(this.emojiLookup.lookup(this.GameData.utilData.types[type].emoji, platform))
							}
							if (type in this.GameData.utilData.types) {
								data.gruntTypeColor = this.GameData.utilData.types[type].color
							}

							// TODO: Migrate reward/lineup building to processor per-language enrichment.
							// The processor currently only sends basic gruntRewards IDs; the structured
							// gruntRewardsList with first/second/third slots, chance percentages, and
							// translated names is too complex to half-migrate. Keep GameData lookups for now.
							let gruntRewards = ''
							let gruntRewardsformNormalised = ''
							const gruntRewardsList = {}
							gruntRewardsList.first = { chance: 100, monsters: [] }
							if (gruntType.encounters && gruntType.encounters.first) {
								if (gruntType.secondReward && gruntType.encounters.second) {
									gruntRewards = '85%: '
									gruntRewardsList.first = { chance: 85, monsters: [] }
									let first = true
									gruntType.encounters.first.forEach((fr) => {
										if (!first) gruntRewards += ', '
										else first = false

										const firstReward = +fr.id
										const firstRewardForm = +fr.form
										const firstRewardMonster = Object.values(this.GameData.monsters).find((mon) => mon.id === firstReward && mon.form.id === firstRewardForm)
										gruntRewardsformNormalised = firstRewardMonster.form.name === 'Normal' ? '' : (`${translator.translate(firstRewardMonster.form.name)} `)
										gruntRewards += gruntRewardsformNormalised + firstRewardMonster ? translator.translate(firstRewardMonster.name) : ''
										gruntRewardsList.first.monsters.push({
											id: firstReward,
											formId: firstRewardForm,
											name: translator.translate(firstRewardMonster.name),
											formName: translator.translate(firstRewardMonster.form.name),
											fullName: gruntRewardsformNormalised + translator.translate(firstRewardMonster.name),
										})
									})
									gruntRewards += '\\n15%: '
									gruntRewardsList.second = { chance: 15, monsters: [] }
									first = true
									gruntType.encounters.second.forEach((sr) => {
										if (!first) gruntRewards += ', '
										else first = false

										const secondReward = +sr.id
										const secondRewardForm = +sr.form
										const secondRewardMonster = Object.values(this.GameData.monsters).find((mon) => mon.id === secondReward && mon.form.id === secondRewardForm)
										gruntRewardsformNormalised = secondRewardMonster.form.name === 'Normal' ? '' : (`${translator.translate(secondRewardMonster.form.name)} `)
										gruntRewards += gruntRewardsformNormalised + secondRewardMonster ? translator.translate(secondRewardMonster.name) : ''
										gruntRewardsList.second.monsters.push({
											id: secondReward,
											formId: secondRewardForm,
											name: translator.translate(secondRewardMonster.name),
											formName: translator.translate(secondRewardMonster.form.name),
											fullName: gruntRewardsformNormalised + translator.translate(secondRewardMonster.name),
										})
									})
								} else {
									let first = true
									gruntType.encounters[gruntType.thirdReward ? 'third' : 'first'].forEach((tr) => {
										if (!first) gruntRewards += ', '
										else first = false

										const reward = +tr.id
										const rewardForm = +tr.form
										const rewardMonster = Object.values(this.GameData.monsters).find((mon) => mon.id === reward && mon.form.id === rewardForm)
										gruntRewardsformNormalised = rewardMonster.form.name === 'Normal' ? '' : (`${translator.translate(rewardMonster.form.name)} `)
										gruntRewards += gruntRewardsformNormalised + rewardMonster ? translator.translate(rewardMonster.name) : ''
										gruntRewardsList.first.monsters.push({
											id: reward,
											formId: rewardForm,
											name: translator.translate(rewardMonster.name),
											formName: translator.translate(rewardMonster.form.name),
											fullName: gruntRewardsformNormalised + translator.translate(rewardMonster.name),
										})
									})
								}
								data.gruntRewards = gruntRewards
								data.gruntRewardsList = gruntRewardsList
							}
						}
						// Lineup 100% of encounter
						let gruntLineupformNormalised = ''
						const gruntLineupList = { confirmed: true, monsters: [] }
						if (data.lineup && data.lineup !== 'null') {
							data.lineup.forEach((lr) => {
								const lineup = +lr.pokemon_id
								const lineupForm = +lr.form
								const lineupMonster = Object.values(this.GameData.monsters).find((mon) => mon.id === lineup && mon.form.id === lineupForm)
								gruntLineupformNormalised = lineupMonster.form.name === 'Normal' ? '' : (`${translator.translate(lineupMonster.form.name)} `)
								gruntLineupList.monsters.push({
									id: lineup,
									formId: lineupForm,
									name: translator.translate(lineupMonster.name),
									formName: translator.translate(lineupMonster.form.name),
									fullName: gruntLineupformNormalised + translator.translate(lineupMonster.name),
								})
							})
							data.gruntLineupList = gruntLineupList
						}
					}

					const view = {
						...geoResult,
						...data,
						time: data.distime,
						tthd: data.tth.days,
						tthh: data.tth.hours,
						tthm: data.tth.minutes,
						tths: data.tth.seconds,
						confirmedTime: data.disappear_time_verified,
						now: new Date(),
						nowISO: new Date().toISOString(),
						genderData: data.genderDataEng ? {
							name: translator.translate(data.genderDataEng.name),
							emoji: translator.translate(this.emojiLookup.lookup(data.genderDataEng.emoji, platform)),
						} : { name: '', emoji: '' },
						areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
					}

					const templateType = 'invasion'
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
				this.log.error(`${data.pokestop_id}: [matched] Can't seem to handle pokestop (user cares): `, e, data)
				return []
			}
		} catch (e) {
			this.log.error(`${data.pokestop_id}: [matched] Can't seem to handle pokestop: `, e, data)
		}
	}
}

module.exports = Invasion
