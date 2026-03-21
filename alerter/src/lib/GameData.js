const fs = require('fs')
const path = require('path')
const { log } = require('./logger')

const RESOURCES_DATA = path.resolve(__dirname, '../../../resources/data')

const GameData = { utilData: JSON.parse(fs.readFileSync(path.join(RESOURCES_DATA, 'util.json'))) }
const neededFiles = ['monsters', 'moves', 'items', 'grunts', 'questTypes', 'types', 'translations']

neededFiles.forEach((file) => {
	try {
		GameData[file] = JSON.parse(fs.readFileSync(path.join(RESOURCES_DATA, `${file}.json`)))
	} catch (e) {
		log.error(`Could not find ${file}.json in resources/data/, before starting Poracle you will need to run 'npm run generate' or change your PM2 script to 'script: "npm start"'`)
		process.exit(9)
	}
})

module.exports = GameData
