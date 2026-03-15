const fs = require('fs')
const path = require('path')
const stripJsonComments = require('strip-json-comments')

// Config directory: derived from CONFIG_PATH env or default location
const configDir = path.dirname(process.env.CONFIG_PATH || path.resolve(__dirname, '../../../config/config.toml'))

// Fallbacks directory: bundled defaults used when user hasn't provided a file in config/
const fallbacksDir = path.resolve(__dirname, '../../../fallbacks')

/**
 * Resolve a config file path. Returns the user's file from config/ if it exists,
 * otherwise falls back to fallbacks/. Returns null if neither exists.
 */
function resolveConfigFile(name) {
	const userFile = path.join(configDir, name)
	if (fs.existsSync(userFile)) return userFile

	const exampleFile = path.join(fallbacksDir, name)
	if (fs.existsSync(exampleFile)) return exampleFile

	return null
}

/**
 * Load and parse a JSON config file (with comment stripping).
 * Uses fallback chain: config/<name> → fallbacks/<name> → null.
 */
function loadConfigJson(name) {
	const filePath = resolveConfigFile(name)
	if (!filePath) return null

	const raw = fs.readFileSync(filePath, 'utf8')
	return JSON.parse(stripJsonComments(raw))
}

/**
 * Load and parse a JSON file from the examples directory only. Returns null if missing.
 */
function loadExampleJson(name) {
	const filePath = path.join(fallbacksDir, name)
	if (!fs.existsSync(filePath)) return null

	const raw = fs.readFileSync(filePath, 'utf8')
	return JSON.parse(stripJsonComments(raw))
}

/**
 * Load a user's config file only (no fallback to examples). Returns null if missing.
 */
function loadUserConfigJson(name) {
	const filePath = path.join(configDir, name)
	if (!fs.existsSync(filePath)) return null

	const raw = fs.readFileSync(filePath, 'utf8')
	return JSON.parse(stripJsonComments(raw))
}

/**
 * List JSON files in a config subdirectory. Returns [] if the directory doesn't exist.
 */
function listConfigDir(name) {
	const dirPath = path.join(configDir, name)
	if (!fs.existsSync(dirPath)) return []

	return fs.readdirSync(dirPath)
		.filter((f) => path.extname(f).toLowerCase() === '.json')
		.map((f) => path.join(dirPath, f))
}

/**
 * Return the resolved config directory path (for chokidar watches etc.)
 */
function getConfigDir() {
	return configDir
}

module.exports = { resolveConfigFile, loadConfigJson, loadExampleJson, loadUserConfigJson, listConfigDir, getConfigDir }
