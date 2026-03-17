const { loadConfigJson } = require('./configResolver')

let aliasCache = null

function getPokemonAlias() {
	if (!aliasCache) {
		aliasCache = loadConfigJson('pokemonAlias.json') || {}
	}
	return aliasCache
}

module.exports = { getPokemonAlias }
