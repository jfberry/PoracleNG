const { loadConfigJson } = require('./configResolver')

let _cache = null

function getPokemonAlias() {
	if (!_cache) {
		_cache = loadConfigJson('pokemonAlias.json') || {}
	}
	return _cache
}

module.exports = { getPokemonAlias }
