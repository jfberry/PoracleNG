const { describe, it } = require('mocha')
const { expect } = require('chai')
const { runCommand } = require('./commandTestHarness')

describe('gym command', () => {
	it('should accept valid args (mystic)', async () => {
		const result = await runCommand('gym', ['mystic'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts).to.have.lengthOf(1)
		expect(result.queries.inserts[0].rows[0].team).to.equal(1)
	})

	it('should accept slot changes', async () => {
		const result = await runCommand('gym', ['mystic', 'slot changes'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts[0].rows[0].slot_changes).to.equal(1)
	})

	it('should report unrecognized args (typo)', async () => {
		const result = await runCommand('gym', ['mystic', 'typoword'])
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('typoword')
	})

	it('should report unrecognized args in remove mode', async () => {
		const result = await runCommand('gym', ['remove', 'everything', 'badarg'])
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('badarg')
	})

	it('should accept everything keyword', async () => {
		const result = await runCommand('gym', ['everything'])
		expect(result.reactions).to.not.include('🙅')
	})
})
