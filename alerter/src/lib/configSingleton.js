const { loadToml } = require('./tomlConfigLoader')
const { adaptConfig } = require('./configAdapter')

let _config = null

function getConfig() {
	if (!_config) {
		const toml = loadToml()
		_config = Object.freeze(adaptConfig(toml))
	}
	return _config
}

// Return a proxy that behaves like the config object itself
// so `require('./configSingleton').database` works just like `require('config').database`
module.exports = new Proxy({}, {
	get(_, prop) {
		if (prop === '__esModule') return false
		if (prop === 'toJSON') return () => getConfig()
		if (prop === '_reload') {
			// Exposed for testing; forces re-read
			return () => { _config = null }
		}
		return getConfig()[prop]
	},
	has(_, prop) {
		return prop in getConfig()
	},
	ownKeys() {
		return Object.keys(getConfig())
	},
	getOwnPropertyDescriptor(_, prop) {
		const cfg = getConfig()
		if (prop in cfg) {
			return { configurable: true, enumerable: true, value: cfg[prop] }
		}
		return undefined
	},
})
