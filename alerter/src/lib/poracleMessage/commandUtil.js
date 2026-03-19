function reportUnrecognizedArgs(msg, translator, args, consumed) {
	const unrecognized = args.filter((a) => !consumed.has(a))
	if (unrecognized.length) {
		msg.react('🙅')
		msg.reply(translator.translateFormat(
			'I do not understand these options: {0}',
			unrecognized.join(', '),
		))
		return true
	}
	return false
}

module.exports = { reportUnrecognizedArgs }
