const fs = require('fs')
const path = require('path')
const { log } = require('./logger')

const RESOURCES_DATA = path.resolve(__dirname, '../../../resources/data')

const GameData = { utilData: JSON.parse(fs.readFileSync(path.join(RESOURCES_DATA, 'util.json'))) }

// Only load files the alerter actually uses:
// - monsters: commands (!track, !nest, !raid, !maxbattle, !tracked, script), evolutionCalculator
// - moves: commands (!track, !maxbattle, !raid, script)
// - items: commands (!quest, script)
// - grunts: commands (!invasion), /api/masterdata/grunts
// Removed: questTypes (unused), types (utilData.types used instead), translations (processor handles)
const neededFiles = ['monsters', 'moves', 'items', 'grunts']

neededFiles.forEach((file) => {
	try {
		GameData[file] = JSON.parse(fs.readFileSync(path.join(RESOURCES_DATA, `${file}.json`)))
	} catch (e) {
		log.error(`Could not find ${file}.json in resources/data/. The processor downloads these at startup — ensure the processor has run at least once.`)
		process.exit(9)
	}
})

module.exports = GameData
