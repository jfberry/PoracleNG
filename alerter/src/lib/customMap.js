const stripJsonComments = require('strip-json-comments')
const fs = require('fs')
const { listConfigDir } = require('./configResolver')

function readCustomMaps() {
	const maps = []

	const files = listConfigDir('customMaps')

	for (const filePath of files) {
		const raw = fs.readFileSync(filePath, 'utf8')
		const mapAddition = JSON.parse(stripJsonComments(raw))
		if (Array.isArray(mapAddition)) {
			maps.push(...mapAddition)
		} else {
			maps.push(mapAddition)
		}
	}

	return maps
}

module.exports = readCustomMaps()
