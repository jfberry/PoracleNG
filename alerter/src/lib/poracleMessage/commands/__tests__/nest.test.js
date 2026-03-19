const { describe, it } = require('mocha')
const { expect } = require('chai')
const { runCommand } = require('./commandTestHarness')

describe('nest command', () => {
	const overrides = {
		GameData: {
			monsters: {
				'1_0': {
					id: 1, name: 'Bulbasaur', form: { id: 0, name: 'Normal' }, types: [{ name: 'Grass' }, { name: 'Poison' }],
				},
			},
			moves: {},
			items: {},
			grunts: {},
			utilData: {
				types: { Grass: true, Poison: true },
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

	it('should accept valid pokemon name', async () => {
		const result = await runCommand('nest', ['bulbasaur'], overrides)
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts).to.have.lengthOf(1)
		expect(result.queries.inserts[0].rows[0].pokemon_id).to.equal(1)
	})

	it('should report unrecognized args (typo)', async () => {
		const result = await runCommand('nest', ['bulbasaur', 'typoword'], overrides)
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('typoword')
	})

	it('should accept everything keyword', async () => {
		const result = await runCommand('nest', ['everything'], overrides)
		expect(result.reactions).to.not.include('🙅')
	})

	it('should accept clean keyword', async () => {
		const result = await runCommand('nest', ['bulbasaur', 'clean'], overrides)
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts[0].rows[0].clean).to.equal(1)
	})

	it('should report unrecognized args in remove mode', async () => {
		const result = await runCommand('nest', ['remove', 'bulbasaur', 'badarg'], overrides)
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('badarg')
	})
})
