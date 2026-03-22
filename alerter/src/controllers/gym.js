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
			data.slotsAvailable = data.slots_available
			data.oldSlotsAvailable = data.old_slots_available
			data.trainerCount = 6 - data.slotsAvailable
			data.oldTrainerCount = 6 - data.oldSlotsAvailable
			data.ex = !!(data.ex_raid_eligible ?? data.is_ex_raid_eligible)
			data.inBattle = data.is_in_battle ?? data.in_battle

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${logReference}: [matched] Gym ${data.name} and ${whoCares.length} humans cared.`)
			} else {
				return []
			}

			try {
				// Icon URLs are pre-computed by the processor enrichment

				const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })
				const jobs = []

				nightTime.setNightTime(data, this.config)

				// Static map is pre-computed by the processor enrichment
				data.staticmap = data.staticMap // deprecated alias
				data.intersection = await this.obtainIntersection(data)

				for (const cares of whoCares) {
					this.log.debug(`${logReference}: [matched] Creating gym alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`, cares)

					this.log.verbose(`${logReference}: [matched] Creating gym alert for ${cares.type} ${cares.id} ${cares.name} ${cares.language} ${cares.template}`)

					const language = cares.language || this.config.general.locale
					const translator = this.translatorFactory.Translator(language)
					let [platform] = cares.type.split(':')
					if (platform === 'webhook') platform = 'discord'

					// Pre-translated names from processor per-language enrichment
					const langEnrichment = this.getLanguageEnrichment(data, language)
					data.teamName = langEnrichment.teamName
					data.gymColor = langEnrichment.teamColor
					data.color = langEnrichment.teamColor
					data.oldTeamName = langEnrichment.oldTeamName
					data.previousControlName = langEnrichment.previousControlName || ''

					// Per-platform emoji lookups (using emoji keys from enrichment)
					data.teamEmojiEng = langEnrichment.teamEmojiKey ? this.emojiLookup.lookup(langEnrichment.teamEmojiKey, platform) : ''
					data.teamEmoji = translator.translate(data.teamEmojiEng)
					data.previousControlTeamEmojiEng = langEnrichment.oldTeamEmojiKey ? this.emojiLookup.lookup(langEnrichment.oldTeamEmojiKey, platform) : ''
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
