const { loadConfigJson } = require('./configResolver')

function registerPartials(handlebars) {
	const partials = loadConfigJson('partials.json')
	if (!partials) return

	for (const [key, partial] of Object.entries(partials)) {
		handlebars.registerPartial(key, partial)
	}
}

exports.registerPartials = registerPartials
