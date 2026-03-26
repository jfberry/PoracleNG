const { describe, it } = require('mocha')
const { expect } = require('chai')
const { runCommand } = require('./commandTestHarness')

describe('incident command', () => {
	const overrides = {
		GameData: {
			monsters: {},
			moves: {},
			items: {},
			grunts: {
				1: { type: 'dragon' },
				2: { type: 'fire' },
				3: { type: 'giovanni' },
			},
			utilData: {
				types: {},
				teams: {
					0: { name: 'Harmony' }, 1: { name: 'Mystic' }, 2: { name: 'Valor' }, 3: { name: 'Instinct' }, 4: { name: 'All' },
				},
				raidLevels: {},
				maxbattleLevels: {},
				genData: {},
				rarity: {},
				size: {},
				pokestopEvent: {},
			},
		},
	}

	it('should accept valid grunt type', async () => {
		const result = await runCommand('incident', ['dragon'], overrides)
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts).to.have.lengthOf(1)
		expect(result.queries.inserts[0].rows[0].grunt_type).to.equal('dragon')
	})

	it('should report unrecognized args (typo)', async () => {
		const result = await runCommand('incident', ['dragon', 'typoword'], overrides)
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('typoword')
	})

	it('should accept everything keyword', async () => {
		const result = await runCommand('incident', ['everything'], overrides)
		expect(result.reactions).to.not.include('🙅')
	})

	it('should accept gender options', async () => {
		const result = await runCommand('incident', ['dragon', 'female'], overrides)
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts[0].rows[0].gender).to.equal(2)
	})

	it('should report unrecognized args in remove mode', async () => {
		const result = await runCommand('incident', ['remove', 'everything', 'badarg'], overrides)
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('badarg')
	})
})
