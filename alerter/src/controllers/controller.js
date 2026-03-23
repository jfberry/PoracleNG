const inside = require('point-in-polygon')
const EventEmitter = require('events')
const path = require('path')
const fs = require('fs')
const { getConfigDir } = require('../lib/configResolver')
const Uicons = require('../lib/uicons')
const replaceAsync = require('../util/stringReplaceAsync')
const HideUriShortener = require('../lib/hideuriUrlShortener')
const ShlinkUriShortener = require('../lib/shlinkUrlShortener')
const YourlsUriShortener = require('../lib/yourlsUrlShortener')
const GetIntersection = require('../lib/getIntersection')

const EmojiLookup = require('../lib/emojiLookup')

class Controller extends EventEmitter {
	constructor(log, db, geocoder, scannerQuery, config, dts, geofence, GameData, discordCache, translatorFactory, mustache, weatherData, statsData, eventProviders) { // discordCache param kept for backward compat but unused
		super()
		this.db = db
		this.scannerQuery = scannerQuery
		this.config = config
		this.log = log
		this.dts = dts
		this.geofence = geofence
		this.GameData = GameData
		this.translatorFactory = translatorFactory
		this.translator = translatorFactory ? this.translatorFactory.default : null
		this.mustache = mustache
		this.weatherData = weatherData
		this.statsData = statsData
		this.shinyPossible = eventProviders && eventProviders.shinyPossible
		//		this.controllerData = weatherCacheData || {}
		this.getIntersection = new GetIntersection(this.config, this.log)
		this.emojiLookup = new EmojiLookup(GameData.utilData.emojis)
		this.imgUicons = this.config.general.imgUrl ? new Uicons((this.config.general.images && this.config.general.images[this.constructor.name.toLowerCase()]) || this.config.general.imgUrl, 'png', this.log) : null
		this.imgUiconsAlt = this.config.general.imgUrlAlt ? new Uicons((this.config.general.imagesAlt && this.config.general.imagesAlt[this.constructor.name.toLowerCase()]) || this.config.general.imgUrlAlt, 'png', this.log) : null
		this.stickerUicons = this.config.general.stickerUrl ? new Uicons((this.config.general.stickers && this.config.general.stickers[this.constructor.name.toLowerCase()]) || this.config.general.stickerUrl, 'webp', this.log) : null
		this.dtsCache = {}
		this.shortener = this.getShortener()
	}

	getShortener() {
		switch (this.config.general.shortlinkProvider) {
			case 'shlink': {
				return new ShlinkUriShortener(this.log, this.config.general.shortlinkProviderURL, this.config.general.shortlinkProviderKey, this.config.general.shortlinkProviderDomain)
			}
			case 'yourls': {
				return new YourlsUriShortener(this.log, this.config.general.shortlinkProviderURL, this.config.general.shortlinkProviderKey)
			}
			default: {
				return new HideUriShortener(this.log)
			}
		}
	}

	setDts(dts) {
		this.dtsCache = { }
		this.dts = dts
	}

	setGeofence(geofence) {
		this.geofence = geofence
	}

	getDts(logReference, templateType, platform, templateName, language) {
		if (!templateName) templateName = this.config.general.defaultTemplateName?.toString() || '1'
		templateName = templateName.toLowerCase()

		const key = `${templateType} ${platform} ${templateName} ${language}`
		if (this.dtsCache[key]) {
			return this.dtsCache[key]
		}

		// Exact match
		let findDts = this.dts.find((template) => template.type === templateType && template.id && template.id.toString().toLowerCase() === templateName.toString() && template.platform === platform && template.language === language)

		// First right template and platform and no language (likely backward compatible choice)
		if (!findDts) {
			findDts = this.dts.find((template) => template.type === templateType && template.id && template.id.toString().toLowerCase() === templateName.toString() && template.platform === platform && !template.language)
		}

		// Default of right template type, platform and language
		if (!findDts) {
			findDts = this.dts.find((template) => template.type === templateType && template.default && template.platform === platform && template.language === language)
		}

		// First default of right template type and platform with empty language
		if (!findDts) {
			findDts = this.dts.find((template) => template.type === templateType && template.default && template.platform === platform && !template.language)
		}

		// First default of right template type and platform
		if (!findDts) {
			findDts = this.dts.find((template) => template.type === templateType && template.default && template.platform === platform)
		}

		if (!findDts) {
			this.log.warn(`${logReference}: Cannot find DTS template or matching default ${key}`)
			return null
		}

		this.log.debug(`${logReference}: Matched to DTS type: ${findDts.type} platform: ${findDts.platform} language: ${findDts.language} template: ${findDts.id}`)

		let template
		if (findDts.templateFile) {
			let filepath
			try {
				filepath = path.join(getConfigDir(), findDts.templateFile)
				template = fs.readFileSync(filepath, 'utf8')
			} catch (err) {
				this.log.error(`${logReference}: Unable to load DTS filepath ${filepath} from DTS type: ${findDts.type} platform: ${findDts.platform} language: ${findDts.language} template: ${findDts.template}`)
				return null
			}
		} else {
			const loadInclude = (includeString) => {
				const includePath = includeString.split(' ')[1]
				const filepath = path.join(getConfigDir(), 'dts', includePath)
				try {
					template = fs.readFileSync(filepath, 'utf8')
				} catch (err) {
					this.log.error(`${logReference}: Unable to load @include ${includePath} filepath ${filepath} from DTS type: ${findDts.type} platform: ${findDts.platform} language: ${findDts.language} template: ${findDts.template}`)
					return `Cannot load @include - ${includeString}`
				}
				return template
			}

			if (findDts.template.embed) {
				for (const field of ['description', 'title']) {
					if (findDts.template.embed[field]) {
						if (Array.isArray(findDts.template.embed[field])) {
							findDts.template.embed[field] = findDts.template.embed[field].join('')
						}
						if (findDts.template.embed[field].startsWith('@include')) {
							findDts.template.embed[field] = loadInclude(findDts.template.embed[field])
						}
					}
				}
			}

			if (Array.isArray(findDts.template.content)) {
				findDts.template.content = findDts.template.content.join('')
			}
			if (findDts.template.content?.startsWith('@include')) {
				findDts.template.content = loadInclude(findDts.template.content)
			}

			template = JSON.stringify(findDts.template)
		}

		const mustache = this.mustache.compile(template)

		this.dtsCache[key] = mustache
		return mustache
	}

	async createMessage(logReference, templateType, platform, template, language, ping, view) {
		const mustache = this.getDts(logReference, templateType, platform, template, language)
		let message
		if (mustache) {
			let mustacheResult
			try {
				mustacheResult = mustache(view, { data: { language, platform } })
			} catch (err) {
				this.log.error(`${logReference}: Error generating mustache results for ${platform}/${templateType}:${template}/${language}`, err, view)
			}
			if (mustacheResult) {
				mustacheResult = await this.urlShorten(mustacheResult)
				try {
					message = JSON.parse(mustacheResult)
					if (ping) {
						if (!message.content) {
							message.content = ping
						} else {
							message.content += ping
						}
					}
				} catch (err) {
					this.log.error(`${logReference}: Error JSON parsing mustache results ${mustacheResult}`, err)
				}
			}
		}

		if (!message) {
			message = { content: `*Poracle*: An alert was triggered with invalid or missing message template - ref: ${logReference}\nid: '${template}' type: '${templateType}' platform: '${platform}' language: '${language}'` }
			this.log.warn(`${logReference}: Invalid or missing message template ref: ${logReference}\nid: '${template}' type: '${templateType}' platform: '${platform}' language: '${language}'`)
		}

		return message
	}

	// eslint-disable-next-line class-methods-use-this
	escapeJsonString(s) {
		if (!s) return s
		return s.replace(/"/g, '\'\'').replace(/\n/g, ' ').replace(/\\/g, '?')
	}

	/**
	 * Replace URLs with shortened versions if surrounded by <S< >S>
	 */
	// eslint-disable-next-line class-methods-use-this
	async urlShorten(s) {
		return replaceAsync(
			s,
			/<S<(.*?)>S>/g,
			async (match, name) => this.shortener.getShortlink(name),
		)
	}

	async obtainIntersection(data) {
		const inte = await this.getIntersection.getIntersection(data.latitude, data.longitude)
		return inte
	}

	pointInArea(point) {
		if (!this.geofence.geofence.length) return []

		const result = this.geofence.rbush.search({
			minX: point[0],
			minY: point[1],
			maxX: point[0],
			maxY: point[1],
		})

		const matchAreas = []

		for (const potential of result) {
			const areaObj = potential.fence

			if (areaObj.path) {
				if (inside(point, areaObj.path)) {
					matchAreas.push({
						name: areaObj.name,
						description: areaObj.description,
						displayInMatches: areaObj.displayInMatches ?? true,
						group: areaObj.group,
					})
				}
			} else if (areaObj.multipath) {
				for (const p of areaObj.multipath) {
					if (inside(point, p)) {
						matchAreas.push({
							name: areaObj.name,
							description: areaObj.description,
							displayInMatches: areaObj.displayInMatches ?? true,
							group: areaObj.group,
						})
						break
					}
				}
			}
		}

		const dedupedList = []

		for (const match of matchAreas) {
			if (!dedupedList.some((x) => x.name === match.name)) {
				dedupedList.push(match)
			}
		}
		return dedupedList
	}

	// database methods below

	async selectOneQuery(table, conditions) {
		try {
			return await this.db.select('*').from(table).where(conditions).first()
		} catch (err) {
			throw { source: 'selectOneQuery', error: err }
		}
	}

	async selectAllQuery(table, conditions) {
		try {
			return await this.db.select('*').from(table).where(conditions)
		} catch (err) {
			throw { source: 'selectAllQuery', error: err }
		}
	}

	async selectAllNotQuery(table, conditions) {
		try {
			return await this.db.select('*').from(table).whereNot(conditions)
		} catch (err) {
			throw { source: 'selectAllNotQuery', error: err }
		}
	}

	async updateQuery(table, values, conditions) {
		try {
			return this.db(table).update(values).where(conditions)
		} catch (err) {
			throw { source: 'updateQuery', error: err }
		}
	}

	async countQuery(table, conditions) {
		try {
			const result = await this.db.select().from(table).where(conditions).count()
				.first()
			return +(Object.values(result)[0])
		} catch (err) {
			throw { source: 'countQuery', error: err }
		}
	}

	async insertQuery(table, values, returning) {
		if (Array.isArray(values) && !values.length) return []
		try {
			const q = this.db.insert(values).into(table)
			if (returning) q.returning(returning)
			return await q
		} catch (err) {
			throw { source: 'insertQuery', error: err }
		}
	}

	async mysteryQuery(sql) {
		try {
			return this.returnByDatabaseType(await this.db.raw(sql))
		} catch (err) {
			throw { source: 'mysteryQuery', error: err }
		}
	}

	async deleteWhereInQuery(table, id, values, valuesColumn) {
		try {
			return this.db.whereIn(valuesColumn, values).where(typeof id === 'object' ? id : { id }).from(table).del()
		} catch (err) {
			throw { source: 'deleteWhereInQuery unhappy', error: err }
		}
	}

	async deleteQuery(table, values) {
		try {
			return await this.db(table).where(values).del()
		} catch (err) {
			throw { source: 'deleteQuery', error: err }
		}
	}

	returnByDatabaseType(data) {
		switch (this.config.database.client) {
			case 'pg': {
				return data.rows
			}
			case 'mysql': {
				return data[0]
			}
			default: {
				return data
			}
		}
	}

	/**
	 * Get per-language enrichment from the processor.
	 * Contains pre-translated game data names (pokemon, moves, types, weather, etc.)
	 * so the controller can skip GameData lookups and translator.translate() calls.
	 * The controller still needs to do emoji lookups per platform.
	 */
	// eslint-disable-next-line class-methods-use-this
	getLanguageEnrichment(data, language) {
		return data.perLanguageEnrichment[language] // eslint-disable-line no-underscore-dangle
	}

	findIvColor(iv) {
		// it must be perfect if none of the ifs kick in
		// orange / legendary
		let colorIdx = 5

		if (iv < 25) colorIdx = 0 // gray / trash / missing
		else if (iv < 50) colorIdx = 1 // white / common
		else if (iv < 82) colorIdx = 2 // green / uncommon
		else if (iv < 90) colorIdx = 3 // blue / rare
		else if (iv < 100) colorIdx = 4 // purple epic

		return this.config.discord.ivColors[colorIdx]
	}
}

module.exports = Controller
