const fs = require('fs')
const path = require('path')
const { log } = require('./logger')

const RESOURCES_DATA = path.resolve(__dirname, '../../../resources/data')

const GameData = {}

// Only load files the alerter actually uses:
// - util: everywhere (emoji, weather, types, genders, etc.)
// - monsters: commands (!track, !nest, !raid, !maxbattle, !tracked, script), evolutionCalculator
// - moves: commands (!track, !maxbattle, !raid, script)
// - items: commands (!quest, script)
// - grunts: commands (!invasion), /api/masterdata/grunts
// Removed: questTypes (unused), types (utilData.types used instead), translations (processor handles)
const neededFiles = ['util', 'monsters', 'moves', 'items', 'grunts']

neededFiles.forEach((file) => {
	const filePath = path.join(RESOURCES_DATA, `${file}.json`)
	try {
		const data = JSON.parse(fs.readFileSync(filePath))
		if (file === 'util') {
			GameData.utilData = data
		} else {
			GameData[file] = data
		}
	} catch (e) {
		log.error(`Could not load ${filePath}. The processor downloads resource files at startup — ensure the processor has started successfully before the alerter.`)
		process.exit(9)
	}
})

module.exports = GameData
