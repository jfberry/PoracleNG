const stripJsonComments = require('strip-json-comments')
const fs = require('fs')
const { resolveConfigFile, listConfigDir } = require('./configResolver')

function readDtsFiles() {
	let localDts = []

	const dtsPath = resolveConfigFile('dts.json')
	if (!dtsPath) {
		throw new Error('dts.json - not found in config/ or fallbacks/')
	}

	try {
		const dtsText = stripJsonComments(fs.readFileSync(dtsPath, 'utf8'))
		localDts = JSON.parse(dtsText)
	} catch (err) {
		throw new Error(`dts.json - ${err.message}`)
	}

	const extraFiles = listConfigDir('dts')

	for (const filePath of extraFiles) {
		let dtsAddition
		try {
			const dtsText = stripJsonComments(fs.readFileSync(filePath, 'utf8'))
			dtsAddition = JSON.parse(dtsText)
		} catch (err) {
			throw new Error(`${filePath} - ${err.message}`)
		}
		localDts = localDts.concat(dtsAddition)
	}

	return localDts
}

module.exports = { readDtsFiles }
