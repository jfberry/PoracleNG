const fs = require('fs')
const path = require('path')
const Fetch = require('node-fetch-native')

const { log } = require('../lib/logger')

const RESOURCES_DIR = path.resolve(__dirname, '../../../resources')
const DATA_DIR = path.join(RESOURCES_DIR, 'data')
const LOCALE_DIR = path.join(RESOURCES_DIR, 'locale')

const fetch = async (url) => {
	try {
		const data = await Fetch(url)
		if (!data.ok) {
			throw new Error(`${data.status} ${data.statusText} URL: ${url}`)
		}
		return await data.json()
	} catch (e) {
		log.warn(e, `Unable to fetch ${url}`)
	}
}

const update = async function update() {
	fs.mkdirSync(DATA_DIR, { recursive: true })
	fs.mkdirSync(LOCALE_DIR, { recursive: true })

	// Write monsters/moves/items/questTypes
	try {
		log.info('Fetching latest Game Master...')
		const gameMaster = await fetch('https://raw.githubusercontent.com/WatWowMap/Masterfile-Generator/master/master-latest-poracle-v2.json')

		log.info('Creating new Game Master...')
		Object.keys(gameMaster).forEach((category) => {
			fs.writeFileSync(
				path.join(DATA_DIR, `${category}.json`),
				JSON.stringify(gameMaster[category], null, 2),
				'utf8',
			)
		})
	} catch (e) {
		log.info('Could not fetch latest GM, using existing...')
	}

	// Write grunts
	if (process.argv[2] === 'latest') {
		try {
			log.info('Fetching latest invasions...')
			fs.writeFileSync(
				path.join(DATA_DIR, 'grunts.json'),
				await fetch('https://raw.githubusercontent.com/WatWowMap/event-info/main/grunts/formatted.json'),
				'utf8',
			)
			log.info('Latest grunts saved...')
		} catch (e) {
			log.warn('Could not generate new invasions, using existing...')
		}
	}

	// Write locales
	try {
		log.info('Creating new locales...')

		const availableLocales = await fetch('https://raw.githubusercontent.com/WatWowMap/pogo-translations/master/index.json')

		await Promise.all(availableLocales.map(async (locale) => {
			try {
				const remoteFiles = await fetch(`https://raw.githubusercontent.com/WatWowMap/pogo-translations/master/static/enRefMerged/${locale}`)
				fs.writeFileSync(
					path.join(LOCALE_DIR, locale),
					JSON.stringify(remoteFiles, null, 2),
					'utf8',
				)
				log.info(`${locale}`, 'file saved.')
			} catch (e) {
				log.warn(`Could not process ${locale}`)
			}
		}))
	} catch (e) {
		log.warn('Could not generate new locales, using existing...')
	}
}

module.exports.update = update

if (require.main === module) {
	update().then(() => { log.info('OK') })
}
