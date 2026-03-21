const Controller = require('./controller')

/**
 * Controller for processing fort update webhooks
 */
class FortUpdate extends Controller {
	async handleMatched(obj, matchedUsers, matchedAreas) {
		const data = obj

		try {
			data.id = data.old?.id || data.new?.id
			const logReference = data.id

			data.longitude = data.new?.location?.lon || data.old?.location?.lon
			data.latitude = data.new?.location?.lat || data.old?.location?.lat

			data.fortType = data.new?.type || data.old?.type || 'unknown'
			Object.assign(data, this.config.general.dtsDictionary)
			data.googleMapUrl = `https://maps.google.com/maps?q=${data.latitude},${data.longitude}`
			data.appleMapUrl = `https://maps.apple.com/place?coordinate=${data.latitude},${data.longitude}`
			data.wazeMapUrl = `https://www.waze.com/ul?ll=${data.latitude},${data.longitude}&navigate=yes&zoom=17`
			if (this.config.general.rdmURL) {
				data.rdmUrl = `${this.config.general.rdmURL}${!this.config.general.rdmURL.endsWith('/') ? '/' : ''}@${data.latitude}/@${data.longitude}/18`
			}
			if (this.config.general.reactMapURL) {
				data.reactMapUrl = `${this.config.general.reactMapURL}${!this.config.general.reactMapURL.endsWith('/') ? '/' : ''}id/${data.fortType}s/${data.id}/18`
			}
			if (this.config.general.rocketMadURL) {
				data.rocketMadUrl = `${this.config.general.rocketMadURL}${!this.config.general.rocketMadURL.endsWith('/') ? '/' : ''}?lat=${data.latitude}&lon=${data.longitude}&zoom=18.0`
			}
			data.name = this.escapeJsonString(data.name)

			// tth, disappearDate, disappearTime, resetDate, resetTime provided by processor enrichment

			data.applemap = data.appleMapUrl // deprecated
			data.mapurl = data.googleMapUrl // deprecated
			data.distime = data.disappearTime // deprecated

			// Use processor-provided matched areas
			data.matchedAreas = matchedAreas || []
			data.matched = data.matchedAreas.map((x) => (x.name || x).toLowerCase())

			// If this is a change from an empty fort (eg after a GMO), treat it as 'new' in poracle
			if (data.change_type === 'edit' && !(data.old?.name || data.old?.description)) {
				data.change_type = 'new'
				data.edit_types = null
			}

			data.changeTypes = []
			if (data.edit_types) data.changeTypes.push(...data.edit_types)
			data.changeTypes.push(data.change_type)
			data.isEmpty = !(data.new?.name || data.new?.description || data.old?.name)

			// clean everything
			if (data.new) {
				if (data.new.name) data.new.name = this.escapeJsonString(data.new.name)
				if (data.new.description) data.new.description = this.escapeJsonString(data.new.description)
			}

			if (data.old) {
				if (data.old.name) data.old.name = this.escapeJsonString(data.old.name)
				if (data.old.description) data.old.description = this.escapeJsonString(data.old.description)
			}

			// helpers
			data.isEdit = data.change_type === 'edit'
			data.isNew = data.change_type === 'new'
			data.isRemoval = data.change_type === 'removal'

			data.isEditLocation = data.changeTypes.includes('location')
			data.isEditName = data.changeTypes.includes('name')
			data.isEditDescription = data.changeTypes.includes('description')
			data.isEditImageUrl = data.changeTypes.includes('image_url')
			data.isEditImgUrl = data.isEditImageUrl

			data.oldName = data.old?.name ?? ''
			data.oldDescription = data.old?.description ?? ''
			data.oldImageUrl = data.old?.image_url ?? ''
			data.oldImgUrl = data.oldImageUrl
			data.oldLatitude = data.old?.location?.lat || 0.0
			data.oldLongitude = data.old?.location?.lon || 0.0

			data.newName = data.new?.name ?? ''
			data.newDescription = data.new?.description ?? ''
			data.newImageUrl = data.new?.image_url ?? ''
			data.newImgUrl = data.newImageUrl
			data.newLatitude = data.new?.location?.lat || 0.0
			data.newLongitude = data.new?.location?.lon || 0.0

			data.fortTypeText = data.fortType === 'pokestop' ? 'Pokestop' : 'Gym'
			// eslint-disable-next-line default-case
			switch (data.change_type) {
				case 'edit':
					data.changeTypeText = 'Edit'
					break
				case 'removal':
					data.changeTypeText = 'Removal'
					break
				case 'new':
					data.changeTypeText = 'New'
					break
			}

			data.name = data.new?.name || data.old?.name || 'unknown'
			data.name = this.escapeJsonString(data.name)
			data.description = data.new?.description || data.old?.description || 'unknown'
			data.imgUrl = data.new?.image_url || data.old?.image_url || ''

			if (data.old) {
				data.old.imgUrl = data.old.image_url
				data.old.imageUrl = data.old.image_url
			}
			if (data.new) {
				data.new.imgUrl = data.new.image_url
				data.new.imageUrl = data.new.image_url
			}

			const whoCares = matchedUsers

			if (whoCares.length) {
				this.log.info(`${logReference}: [matched] Fort Update ${data.fortType} ${data.id} ${data.name} and ${whoCares.length} humans cared.`)
			} else {
				return []
			}

			data.stickerUrl = data.imgUrl

			const geoResult = await this.getAddress({ lat: data.latitude, lon: data.longitude })
			const jobs = []

			// Attempt to calculate best position for map
			const markers = []
			if (data.old?.location?.lat) {
				markers.push({ latitude: data.old.location.lat, longitude: data.old.location.lon })
			}
			if (data.new?.location?.lat) {
				markers.push({ latitude: data.new.location.lat, longitude: data.new.location.lon })
			}

			const position = this.tileserverPregen.autoposition({
				markers,
			}, 500, 250)
			data.zoom = Math.min(position.zoom, 16)
			data.map_longitude = position.longitude
			data.map_latitude = position.latitude

			await this.getStaticMapUrl(logReference, data, 'fort-update', ['map_latitude', 'map_longitude', 'longitude', 'latitude', 'zoom', 'imgUrl', 'isEditLocation', 'oldLatitude', 'oldLongitude', 'newLatitude', 'newLongitude'])
			data.staticmap = data.staticMap // deprecated

			for (const cares of whoCares) {
				this.log.debug(`${logReference}: [matched] Creating fort update alert for ${cares.id} ${cares.name} ${cares.type} ${cares.language} ${cares.template}`, cares)

				this.log.verbose(`${logReference}: [matched] Creating fort update alert for ${cares.type} ${cares.id} ${cares.name} ${cares.language} ${cares.template}`)

				const language = cares.language || this.config.general.locale
				let [platform] = cares.type.split(':')
				if (platform === 'webhook') platform = 'discord'

				const view = {
					...geoResult,
					...data,
					tthd: data.tth?.days || 0,
					tthh: data.tth?.hours || 0,
					tthm: data.tth?.minutes || 0,
					tths: data.tth?.seconds || 0,
					time: data.resetTime || '',
					nowISO: new Date().toISOString(),
					areas: data.matchedAreas.filter((area) => area.displayInMatches).map((area) => area.name).join(', '),
				}

				const templateType = 'fort-update'
				const message = await this.createMessage(logReference, templateType, platform, cares.template, language, cares.ping, view)

				const work = {
					lat: data.latitude.toString().substring(0, 8),
					lon: data.longitude.toString().substring(0, 8),
					message,
					target: cares.id,
					type: cares.type,
					name: cares.name,
					tth: data.tth,
					clean: false,
					emoji: data.emoji,
					logReference,
					language,
				}

				jobs.push(work)
			}

			return jobs
		} catch (e) {
			this.log.error(`${data.id}: [matched] Can't seem to handle fort update: `, e, data)
			return []
		}
	}
}

module.exports = FortUpdate
