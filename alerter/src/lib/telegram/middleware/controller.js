/* eslint no-param-reassign: ["error", { "props": false }] */
module.exports = (query, scannerQuery, logs, GameData, PoracleInfo, geofence, config, re, translatorFactory, emojiStrip) => (ctx, next) => {
	ctx.state.controller = {
		query,
		scannerQuery,
		logs,
		GameData,
		PoracleInfo,
		geofence,
		config,
		re,
		translatorFactory,
		emojiStrip,
	}
	ctx.state.controller.translator = translatorFactory.default
	return next()
}
