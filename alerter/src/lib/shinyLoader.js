const axios = require('axios')

/**
 * Provides shiny-possible lookup, fetching data from the processor API.
 */
class ShinyPossible {
	constructor(log) {
		this.log = log
	}

	/**
	 * Fetch shiny-possible map from processor API
	 * @param {string} processorUrl
	 * @returns {Promise<any>}
	 */
	// eslint-disable-next-line class-methods-use-this
	async download(processorUrl, headers) {
		const resp = await axios.get(`${processorUrl}/api/stats/shiny-possible`, { timeout: 5000, headers })
		return resp.data
	}

	/**
	 * Set parser to use given shiny list
	 * @param shinyPossibleMap
	 */
	loadMap(shinyPossibleMap) {
		this.shinyPossibleMap = shinyPossibleMap
	}

	/**
	 * @param pokemonId
	 * @param formId
	 * @returns {boolean}
	 */
	isShinyPossible(pokemonId, formId) {
		if (!this.shinyPossibleMap) return false

		try {
			if (!this.shinyPossibleMap.map) return false
			if (`${pokemonId}_${formId}` in this.shinyPossibleMap.map) return true
			if (pokemonId in this.shinyPossibleMap.map) return true

			return false
		} catch (err) {
			this.log.error('ShinyPossible: Error parsing shiny file', err)
		}
	}
}

module.exports = ShinyPossible
