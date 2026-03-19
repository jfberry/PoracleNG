const path = require('path')
const config = require('../lib/configSingleton')
const { getConfigDir } = require('../lib/configResolver')

function getCacheDir() {
	const configured = config.geofence?.kojiOptions?.cacheDir
	const cacheDir = configured || '.cache/geofences'
	return path.isAbsolute(cacheDir)
		? cacheDir
		: path.resolve(getConfigDir(), cacheDir)
}

function sanitizeURL(url) {
	return url.replace(/\//g, '__')
}

module.exports = { getCacheDir, sanitizeURL }
