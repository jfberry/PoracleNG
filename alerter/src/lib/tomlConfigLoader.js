const fs = require('fs')
const path = require('path')

let smolToml

function loadSmolToml() {
	if (!smolToml) {
		smolToml = require('smol-toml')
	}
	return smolToml
}

function loadToml(configPath) {
	if (!configPath) {
		configPath = process.env.CONFIG_PATH || path.resolve(__dirname, '../../../config/config.toml')
	}
	const content = fs.readFileSync(configPath, 'utf8')
	return loadSmolToml().parse(content)
}

module.exports = { loadToml }
