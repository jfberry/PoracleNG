const { describe, it } = require('mocha')
const { expect } = require('chai')
const { runCommand } = require('./commandTestHarness')

describe('fort command', () => {
	it('should accept valid args (pokestop location)', async () => {
		const result = await runCommand('fort', ['pokestop', 'location'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts).to.have.lengthOf(1)
	})

	it('should accept everything with changes', async () => {
		const result = await runCommand('fort', ['everything', 'name', 'photo'])
		expect(result.reactions).to.not.include('🙅')
	})

	it('should report unrecognized args (typo)', async () => {
		const result = await runCommand('fort', ['pokestop', 'typoword'])
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('typoword')
	})

	it('should report unrecognized args in remove mode', async () => {
		const result = await runCommand('fort', ['remove', 'everything', 'badarg'])
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('badarg')
	})
})
