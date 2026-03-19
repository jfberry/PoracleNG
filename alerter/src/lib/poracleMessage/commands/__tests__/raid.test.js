const { describe, it } = require('mocha')
const { expect } = require('chai')
const { runCommand } = require('./commandTestHarness')

describe('raid command', () => {
	it('should accept valid args (level5)', async () => {
		const result = await runCommand('raid', ['level5'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts).to.have.lengthOf(1)
	})

	it('should accept level with team and clean', async () => {
		const result = await runCommand('raid', ['level5', 'valor', 'clean'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts).to.have.lengthOf(1)
		const row = result.queries.inserts[0].rows[0]
		expect(row.team).to.equal(2)
		expect(row.clean).to.equal(1)
	})

	it('should report unrecognized args (typo)', async () => {
		const result = await runCommand('raid', ['level5', 'artcuno'])
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('artcuno')
	})

	it('should report unrecognized args mixed with valid', async () => {
		const result = await runCommand('raid', ['level5', 'clean', 'badarg'])
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('badarg')
	})

	it('should report unrecognized args in remove mode', async () => {
		const result = await runCommand('raid', ['remove', 'level5', 'badarg'])
		expect(result.reactions).to.include('🙅')
		const errorReply = result.replies.find((r) => r.text.includes('I do not understand'))
		expect(errorReply).to.not.equal(undefined)
		expect(errorReply.text).to.include('badarg')
	})

	it('should accept everything keyword', async () => {
		const result = await runCommand('raid', ['everything'])
		expect(result.reactions).to.not.include('🙅')
	})

	it('should accept rsvp args', async () => {
		const result = await runCommand('raid', ['level5', 'rsvp'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts[0].rows[0].rsvp_changes).to.equal(1)
	})

	it('should accept ex keyword', async () => {
		const result = await runCommand('raid', ['level5', 'ex'])
		expect(result.reactions).to.not.include('🙅')
		expect(result.queries.inserts[0].rows[0].exclusive).to.equal(1)
	})
})
