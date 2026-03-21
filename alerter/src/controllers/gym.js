const Controller = require('./controller')
const nightTime = require('./common/nightTime')

/**
 * Controller for processing gym webhooks
 * Alerts on Team change
 */
class Gym extends Controller {
	async handleMatched(obj, matchedUsers, matchedAreas) {
		const data = obj
		const minTth = this.config.general.alertMinimumTime || 0

		try {
			const logReference = data.id || data.gym_id

			Object.assign(data, this.config.general.dtsDictionary)
			data.gymId = data.id || data.gym_id
			data.googleMapUrl = `https://maps.google.com/maps?q=${data.latitude},${data.longitude}`
			data.appleMapUrl = `https://maps.apple.com/place?coordinate=${data.latitude},${data.longitude}`
			data.wazeMapUrl = `https://www.waze.com/ul?ll=${data.latitude},${data.longitude}&navigate=yes&zoom=17`
			if (this.config.general.rdmURL) {
				data.rdmUrl = `${this.config.general.rdmURL}${!this.config.general.rdmURL.endsWith('/') ? '/' : ''}@gym/${data.gymId}`
			}
			if (this.config.general.reactMapURL) {
				data.reactMapUrl = `${this.config.general.reactMapURL}${!this.config.general.reactMapURL.endsWith('/') ? '/' : ''}id/gyms/${data.gymId}`
			}
			if (this.config.general.rocketMadURL) {
				data.rocketMadUrl = `${this.config.general.rocketMadURL}${!this.config.general.rocketMadURL.endsWith('/') ? '/' : ''}?lat=${data.latitude}&lon=${data.longitude}&zoom=18.0`
			}
			if (data.gym_name) data.name = data.gym_name
			data.name = this.escapeJsonString(data.name)
			data.gymName = data.name
			data.gymUrl = data.url

			// tth and conqueredTime provided by processor enrichment
			data.applemap = data.appleMapUrl // deprecated
			data.mapurl = data.googleMapUrl // deprecated
			data.distime = data.disappearTime // deprecated

			// Stop handling if it already disappeared or is about to go away
			if (data.tth.firstDateWasLater || ((data.tth.hours * 3600) + (data.tth.minutes * 60) + data.tth.seconds) < minTth) {
				this.log.debug(`${data.id} [matched] Gym already disappeared or is about to go away in: ${data.tth.hours}:${data.tth.minutes}:${data.tth.seconds}`)
				return []
			}

			// Use processor-provided matched areas
			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			data.teamId = data.team_id ?? data.team ?? 0
			data.oldTeamId = data.old_team_id ?? 0
			data.previousControlId = data.last_owner_id ?? 0
			data.teamNameEng = this.GameData.utilData.teams[data.teamId].name
			data.oldTeamNameEng = data.old_team_id >= 0 ? this.GameData.utilData.teams[data.old_team_id].name : ''
			data.previousControlNameEng = data.last_owner_id >= 0 ? this.GameData.utilData.teams[data.last_owner_id].name : ''
			data.gymColor = this.GameData.utilData.teams[data.teamId].color
			data.slotsAvailable = data.slots_available
			data.oldSlotsAvailable = data.old_slots_available
			data.trainerCount = 6 - data.slotsAvailable
			data.oldTrainerCount = 6 - data.oldSlotsAvailable
			data.ex = !!(data.ex_raid_eligible ?? data.is_ex_raid_eligible)
			data.color = data.gymColor
			data.inBattle = data.is_in_battle ?? data.in_battle

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${logReference}: [matched] Gym ${data.name} and ${whoCares.length} humans cared.`)
			} else {
				return []
			}

			try {
				if (this.imgUicons) data.imgUrl = await this.imgUicons.gymIcon(data.teamId, 6 - data.slotsAvailable, data.inBattle, data.ex) || this.config.fallbacks?.imgUrlGym
				if (this.stickerUicons) data.stickerUrl = await this.stickerUicons.gymIcon(data.teamId, 6 - data.slotsAvailable, data.inBattle, data.ex)

				const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })
				const jobs = []

				nightTime.setNightTime(data, this.config)

				await this.getStaticMapUrl(logReference, data, 'gym', ['teamId', 'latitude', 'longitude', 'imgUrl', 'style'])
				data.intersection = await this.obtainIntersection(data)

				data.staticmap = data.staticMap // deprecated

				for (const cares of whoCares) {
					this.log.debug(`${logReference}: [matched] Creating gym alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`, cares)

					this.log.verbose(`${logReference}: [matched] Creating gym alert for ${cares.type} ${cares.id} ${cares.name} ${cares.language} ${cares.template}`)

					const language = cares.language || this.config.general.locale
					const translator = this.translatorFactory.Translator(language)
					let [platform] = cares.type.split(':')
					if (platform === 'webhook') platform = 'discord'

					data.teamName = translator.translate(data.teamNameEng)
					data.oldTeamName = translator.translate(data.oldTeamNameEng)
					data.previousControlName = translator.translate(data.previousControlNameEng)
					data.teamEmojiEng = data.teamId >= 0 ? this.emojiLookup.lookup(this.GameData.utilData.teams[data.teamId].emoji, platform) : ''
					data.teamEmoji = translator.translate(data.teamEmojiEng)
					data.previousControlTeamEmojiEng = data.previousControlId >= 0 ? this.emojiLookup.lookup(this.GameData.utilData.teams[data.previousControlId].emoji, platform) : ''
					data.previousControlTeamEmoji = translator.translate(data.previousControlTeamEmojiEng)

					const view = {
						...geoResult,
						...data,
						time: data.distime,
						tthh: data.tth.hours,
						tthm: data.tth.minutes,
						tths: data.tth.seconds,
						now: new Date(),
						nowISO: new Date().toISOString(),
						areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
					}

					const templateType = 'gym'
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
				this.log.error(`${data.id || data.gym_id}: [matched] Can't seem to handle gym (user cares): `, e, data)
				return []
			}
		} catch (e) {
			this.log.error(`${data.id || data.gym_id}: [matched] Can't seem to handle gym: `, e, data)
		}
	}
}

module.exports = Gym
