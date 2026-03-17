const path = require('path')
const config = require('../lib/configSingleton')

const DEFAULT_CACHE_DIR = path.resolve(__dirname, '../../../config/.cache/geofences')

function getCacheDir() {
	const configured = config.geofence?.kojiOptions?.cacheDir
	if (configured) {
		return path.isAbsolute(configured)
			? configured
			: path.resolve(__dirname, '../../..', configured)
	}
	return DEFAULT_CACHE_DIR
}

function sanitizeURL(url) {
	return url.replace(/\//g, '__')
}

module.exports = { getCacheDir, sanitizeURL }
