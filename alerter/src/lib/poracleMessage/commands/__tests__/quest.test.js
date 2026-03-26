const { describe, it } = require('mocha')
const { expect } = require('chai')
const { runCommand } = require('./commandTestHarness')

describe('quest command', () => {
	it('should accept stardust keyword', async () => {
		const result = await runCommand('quest', ['stardust'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts).to.have.lengthOf(1)
		expect(result.queries.inserts[0].rows[0].reward_type).to.equal(3)
	})

	it('should accept energy keyword', async () => {
		const result = await runCommand('quest', ['energy'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts).to.have.lengthOf(1)
		expect(result.queries.inserts[0].rows[0].reward_type).to.equal(12)
	})

	it('should accept candy keyword', async () => {
		const result = await runCommand('quest', ['candy'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts).to.have.lengthOf(1)
		expect(result.queries.inserts[0].rows[0].reward_type).to.equal(4)
	})

	it('should report unrecognized args (typo)', async () => {
		const result = await runCommand('quest', ['stardust', 'typoword'])
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('typoword')
	})

	it('should report unrecognized args in remove mode', async () => {
		const result = await runCommand('quest', ['remove', 'stardust', 'badarg'])
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('badarg')
	})

	it('should accept shiny keyword', async () => {
		const result = await runCommand('quest', ['stardust', 'shiny'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts[0].rows[0].shiny).to.equal(1)
	})

	it('should accept clean keyword', async () => {
		const result = await runCommand('quest', ['stardust', 'clean'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts[0].rows[0].clean).to.equal(1)
	})
})
