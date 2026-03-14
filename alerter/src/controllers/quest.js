//
// Quest controller getReward and getQuest function inspired from PMSF
//
// ....because it is smartly done there!!

const Controller = require('./controller')
const { log } = require('../lib/logger')

class Quest extends Controller {
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
			data.intersection = await this.obtainIntersection(data)
			// disappearTime is pre-computed by the Go processor
			data.applemap = data.appleMapUrl // deprecated
			data.mapurl = data.googleMapUrl // deprecated
			data.disTime = data.disappearTime // deprecated
			// tth is pre-computed by the Go processor
			data.imgUrl = ''
			data.stickerUrl = ''

			if (data.pokestop_name) {
				data.pokestop_name = this.escapeJsonString(data.pokestop_name)
				data.pokestopName = data.pokestop_name
			}
			data.pokestop_url = data.pokestop_url || this.config.fallbacks?.pokestopUrl
			data.pokestopUrl = data.pokestop_url

			if (data.tth.firstDateWasLater || ((data.tth.hours * 3600) + (data.tth.minutes * 60) + data.tth.seconds) < minTth) {
				log.debug(`${data.pokestop_id}: [matched] quest already disappeared or is about to expire in: ${data.tth.hours}:${data.tth.minutes}:${data.tth.seconds}`)
				return []
			}

			data.questStringEng = await this.getQuest(data, 'en')
			data.questString = await this.getQuest(data, this.config.general.locale)
			data.rewardData = await this.getReward(logReference, data)
			this.log.debug(`${logReference} [matched] Quest: data.questString: ${data.questString}, data.rewardData: ${JSON.stringify(data.rewardData)}`)
			data.dustAmount = data.rewardData.dustAmount
			data.isShiny = data.rewardData.monsters.length > 0 ? data.rewardData.monsters[0].shiny : 0
			data.shinyPossible = data.rewardData.monsters.length > 0 ? this.shinyPossible.isShinyPossible(data.rewardData.monsters[0].pokemonId, data.rewardData.monsters[0].formId) : false

			data.itemAmount = data.rewardData.itemAmount
			data.energyAmount = data.rewardData.energyMonsters.length > 0 ? data.rewardData.energyMonsters[0].amount : 0 // deprecated
			data.candyAmount = data.rewardData.candy.length > 0 ? data.rewardData.candy[0].amount : 0 // deprecated

			// Use processor-provided matched areas
			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${logReference}: [matched] Quest appeared and ${whoCares.length} humans cared.`)
			} else {
				return []
			}

			let discordCacheBad = true
			whoCares.forEach((cares) => {
				if (!this.isRateLimited(cares.id)) discordCacheBad = false
			})

			if (discordCacheBad) return []

			data.imgUrl = 'https://s3.amazonaws.com/com.cartodb.users-assets.production/production/jonmrich/assets/20150203194453red_pin.png'
			data.stickerUrl = ''

			if (data.rewardData.monsters.length > 0) {
				if (this.imgUicons) data.imgUrl = await this.imgUicons.pokemonIcon(data.rewardData.monsters[0].pokemonId, data.rewardData.monsters[0].formId, 0, 0, 0, 0, data.isShiny || (data.shinyPossible && this.config.general.requestShinyImages))
				if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.pokemonIcon(data.rewardData.monsters[0].pokemonId, data.rewardData.monsters[0].formId, 0, 0, 0, 0, data.isShiny || (data.shinyPossible && this.config.general.requestShinyImages))
				if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.pokemonIcon(data.rewardData.monsters[0].pokemonId, data.rewardData.monsters[0].formId, 0, 0, 0, 0, data.isShiny || (data.shinyPossible && this.config.general.requestShinyImages))
			}

			if (data.rewardData.items.length > 0) {
				if (this.imgUicons) data.imgUrl = await this.imgUicons.rewardItemIcon(data.rewardData.items[0].id, data.rewardData.items[0].amount)
				if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.rewardItemIcon(data.rewardData.items[0].id, data.rewardData.items[0].amount)
				if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.rewardItemIcon(data.rewardData.items[0].id, data.rewardData.items[0].amount)
			}
			if (data.dustAmount) {
				if (this.imgUicons) data.imgUrl = await this.imgUicons.rewardStardustIcon(data.rewardData.dustAmount)
				if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.rewardStardustIcon(data.rewardData.dustAmount)
				if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.rewardStardustIcon(data.rewardData.dustAmount)
			}
			if (data.rewardData.energyMonsters.length > 0) {
				if (this.imgUicons) data.imgUrl = await this.imgUicons.rewardMegaEnergyIcon(data.rewardData.energyMonsters[0].pokemonId, data.rewardData.energyMonsters[0].amount)
				if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.rewardMegaEnergyIcon(data.rewardData.energyMonsters[0].pokemonId, data.rewardData.energyMonsters[0].amount)
				if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.rewardMegaEnergyIcon(data.rewardData.energyMonsters[0].pokemonId, data.rewardData.energyMonsters[0].amount)
			}
			if (data.rewardData.candy.length > 0) {
				if (this.imgUicons) data.imgUrl = await this.imgUicons.rewardCandyIcon(data.rewardData.candy[0].pokemonId, data.rewardData.candy[0].amount)
				if (this.imgUiconsAlt) data.imgUrlAlt = await this.imgUiconsAlt.rewardCandyIcon(data.rewardData.candy[0].pokemonId, data.rewardData.candy[0].amount)
				if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.rewardCandyIcon(data.rewardData.candy[0].pokemonId, data.rewardData.candy[0].amount)
			}

			data.imgUrl = data.imgUrl || this.config.fallbacks?.imgUrlPokestop
			data.imgUrlAlt = data.imgUrlAlt || this.config.fallbacks?.imgUrlPokestop

			const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })
			const jobs = []

			await this.getStaticMapUrl(logReference, data, 'quest', ['latitude', 'longitude', 'imgUrl'])
			data.staticmap = data.staticMap // deprecated

			if (data.rewardData.monsters.length > 0) {
				data.baseStats = Object.values(this.GameData.monsters).some((mon) => data.rewardData.monsters[0].pokemonId === mon.id && data.rewardData.monsters[0].formId === mon.form.id) ? Object.values(this.GameData.monsters).filter((mon) => data.rewardData.monsters[0].pokemonId === mon.id && data.rewardData.monsters[0].formId === mon.form.id)[0].stats : ''
				if (!data.baseStats) data.baseStats = Object.values(this.GameData.monsters).some((mon) => data.rewardData.monsters[0].pokemonId === mon.id && !mon.form.id) ? Object.values(this.GameData.monsters).filter((mon) => data.rewardData.monsters[0].pokemonId === mon.id && !mon.form.id)[0].stats : ''
			}

			// Future event fields are pre-computed by the Go processor enrichment

			for (const cares of whoCares) {
				this.log.debug(`${logReference}: [matched] Creating quest alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`, cares)

				const rateLimitTtr = this.getRateLimitTimeToRelease(cares.id)
				if (rateLimitTtr) {
					this.log.verbose(`${logReference}: [matched] Not creating quest alert (Rate limit) for ${cares.type} ${cares.id} ${cares.name} Time to release: ${rateLimitTtr}`)
					// eslint-disable-next-line no-continue
					continue
				}
				this.log.verbose(`${logReference}: [matched] Creating quest alert for ${cares.type} ${cares.id} ${cares.name} ${cares.language} ${cares.template}`)

				const language = cares.language || this.config.general.locale
				const translator = this.translatorFactory.Translator(language)
				let [platform] = cares.type.split(':')
				if (platform === 'webhook') platform = 'discord'
				if (language !== this.config.general.locale) {
					data.questString = await this.getQuest(data, language)
				}

				for (const monster of data.rewardData.monsters) {
					let monsterName
					let formName

					const mon = Object.values(this.GameData.monsters).find((m) => m.id === monster.pokemonId && m.form.id === monster.formId)
					if (!mon) {
						monsterName = `${translator.translate('Unknown monster')} ${monster.pokemonId}`
						formName = `${monster.formId}`
					} else {
						monsterName = mon.name
						formName = mon.form.name
						if (formName === undefined || formName === 'Normal') formName = ''
					}

					monster.nameEng = monsterName
					monster.formEng = formName
					monster.name = translator.translate(monsterName)
					monster.form = translator.translate(formName)
					monster.fullNameEng = monster.nameEng.concat(monster.formEng ? ' ' : '', monster.formEng)
					monster.fullName = monster.name.concat(monster.form ? ' ' : '', monster.form)
				}

				data.monsterNames = data.rewardData.monsters.map((mon) => mon.fullName).join(', ')
				data.monsterNamesEng = data.rewardData.monsters.map((mon) => mon.fullNameEng).join(', ')

				for (const item of data.rewardData.items) {
					const i = this.GameData.items[item.id]
					let itemName
					if (!i) {
						itemName = `${translator.translate('Unknown item')} ${item.id}`
					} else {
						itemName = i.name
					}
					item.nameEng = itemName
					item.name = translator.translate(itemName)
				}

				data.itemNames = data.rewardData.items.map((item) => `${item.amount} ${item.name}`).join(', ')
				data.itemNamesEng = data.rewardData.items.map((item) => `${item.amount} ${item.nameEng}`).join(', ')

				for (const monster of data.rewardData.energyMonsters) {
					let monsterName

					const mon = Object.values(this.GameData.monsters).find((m) => m.id === monster.pokemonId && !m.form.id)
					if (!mon) {
						monsterName = `${translator.translate('Unknown monster')} ${monster.pokemonId}`
					} else {
						monsterName = mon.name
					}

					monster.nameEng = monsterName
					monster.name = translator.translate(monsterName)
				}

				data.energyMonstersNames = data.rewardData.energyMonsters.map((item) => `${item.amount} ${item.name} ${translator.translate('Mega Energy')}`).join(', ')
				data.energyMonstersNamesEng = data.rewardData.energyMonsters.map((item) => `${item.amount} ${item.nameEng} Mega Energy`).join(', ')

				for (const monster of data.rewardData.candy) {
					let monsterName

					const mon = Object.values(this.GameData.monsters).find((m) => m.id === monster.pokemonId && !m.form.id)
					if (!mon) {
						monsterName = `${translator.translate('Unknown monster')} ${monster.pokemonId}`
					} else {
						monsterName = mon.name
					}

					monster.nameEng = monsterName
					monster.name = translator.translate(monsterName)
				}

				data.candyMonstersNames = data.rewardData.candy.map((item) => `${item.amount} ${item.name}  ${translator.translate('Candy')}`).join(', ')
				data.candyMonstersNamesEng = data.rewardData.candy.map((item) => `${item.amount} ${item.nameEng} Candy`).join(', ')

				data.rewardString = [
					data.monsterNames,
					data.dustAmount > 0 ? `${data.dustAmount} ${translator.translate('Stardust')}` : '',
					data.itemNames,
					data.energyMonstersNames,
					data.candyMonstersNames,
				].filter((x) => x).join(', ')

				data.rewardStringEng = [
					data.monsterNamesEng,
					data.dustAmount > 0 ? `${data.dustAmount} Stardust` : '',
					data.itemNamesEng,
					data.energyMonstersNamesEng,
					data.candyMonstersNamesEng,
				].filter((x) => x).join(', ')

				data.shinyPossibleEmoji = data.shinyPossible ? translator.translate(this.emojiLookup.lookup('shiny', platform)) : ''

				const view = {
					...geoResult,
					...data,
					...data.rewardData,
					lat: +data.latitude.toFixed(4),
					lon: +data.longitude.toFixed(4),
					time: data.disappearTime,
					tthh: data.tth.hours,
					tthm: data.tth.minutes,
					tths: data.tth.seconds,
					confirmedTime: data.disappear_time_verified,
					now: new Date(),
					nowISO: new Date().toISOString(),
					areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
				}

				const templateType = 'quest'
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
					logReference,
					language,
				}
				jobs.push(work)
			}

			return jobs
		} catch (e) {
			this.log.error(`${data.pokestop_id}: [matched] Can't seem to handle quest: `, e, data)
		}
	}

	async getQuest(item, language) {
		let str
		if (item.quest_task && !this.config.general.ignoreMADQuestString) {
			str = item.quest_task
		} else {
			language = !this.GameData.translations[language] ? 'en' : language
			const questinfo = `quest_title_${item.title.toLowerCase()}`
			const questTitle = this.GameData.translations[language].questTitles
			if (item.title) {
				try {
					str = questTitle[questinfo]
					if (str.toLowerCase().includes('{{amount_0}}') && item.target) {
						str = str.replace('{{amount_0}}', item.target)
					}
				} catch {
					str = this.GameData.translations[language].questTypes.quest_0
					this.log.warn(`Missing Task for ${questinfo}`)
				}
			}
		}
		return str
	}

	// eslint-disable-next-line class-methods-use-this
	async getReward(logReference, item) {
		const monsters = []
		const items = []
		let dustAmount = 0
		const energyMonsters = []
		const candy = []

		item.rewards.forEach((reward) => {
			if (reward.type === 2) {
				items.push({
					id: reward.info.item_id.toString(),
					amount: reward.info.amount,
				})
			} else if (reward.type === 3) {
				dustAmount = reward.info.amount
			} else if (reward.type === 4) {
				candy.push({
					pokemonId: reward.info.pokemon_id,
					amount: reward.info.amount,
				})
			} else if (reward.type === 7) {
				monsters.push({
					pokemonId: reward.info.pokemon_id,
					formId: reward.info.form_id ?? 0,
					shiny: reward.info.shiny ?? false,
				})
			} else if (reward.type === 12) {
				energyMonsters.push({
					pokemonId: reward.info.pokemon_id,
					amount: reward.info.amount,
				})
			}
		})
		return {
			items, dustAmount, energyMonsters, candy, monsters,
		}
	}
}

module.exports = Quest
