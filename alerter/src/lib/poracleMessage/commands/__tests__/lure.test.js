const { describe, it } = require('mocha')
const { expect } = require('chai')
const { runCommand } = require('./commandTestHarness')

describe('lure command', () => {
	it('should accept valid args (mossy)', async () => {
		const result = await runCommand('lure', ['mossy'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts).to.have.lengthOf(1)
		expect(result.queries.inserts[0].rows[0].lure_id).to.equal(503)
	})

	it('should accept multiple valid lure types', async () => {
		const result = await runCommand('lure', ['mossy', 'glacial', 'clean'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts).to.have.lengthOf(1)
		expect(result.queries.inserts[0].rows).to.have.lengthOf(2)
	})

	it('should report unrecognized args (typo)', async () => {
		const result = await runCommand('lure', ['mossy', 'typoword'])
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('typoword')
	})

	it('should report unrecognized args in remove mode', async () => {
		const result = await runCommand('lure', ['remove', 'mossy', 'badarg'])
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('badarg')
	})

	it('should accept everything keyword', async () => {
		const result = await runCommand('lure', ['everything'])
		expect(result.reactions).to.not.include('🙅')
	})
})
