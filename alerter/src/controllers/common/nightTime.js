/* Night Time style selection */

/**
 * Set the map style based on pre-computed sun time booleans from the Go processor.
 * data.nightTime, data.dawnTime, data.duskTime are set by the processor.
 * This function only selects the appropriate map style from config.
 */
function setNightTime(data, config) {
	const defaultStyle = 'klokantech-basic'

	if (data.dawnTime) {
		data.style = config.geocoding.dawnStyle || defaultStyle
	} else if (data.duskTime) {
		data.style = config.geocoding.duskStyle || defaultStyle
	} else if (data.nightTime) {
		data.style = config.geocoding.nightStyle || defaultStyle
	} else {
		data.style = config.geocoding.dayStyle || defaultStyle
	}
}

module.exports = { setNightTime }
