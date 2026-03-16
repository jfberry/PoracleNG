const PoracleTelegramMessage = require('../poracleTelegramMessage')
const PoracleTelegramState = require('../poracleTelegramState')
const commandLogic = require('../../poracleMessage/commands/maxbattle')

module.exports = async (ctx) => {
	if (Object.keys(ctx.update).includes('channel_post')) return

	try {
		const ptm = new PoracleTelegramMessage(ctx)
		const pts = new PoracleTelegramState(ctx)

		const command = ptm.Pokemon
		for (const c of command.splitArgsArray) {
			await commandLogic.run(pts, ptm, c)
		}
	} catch (err) {
		ctx.controller.logs.telegram.error('Maxbattle command unhappy:', err)
	}
}
