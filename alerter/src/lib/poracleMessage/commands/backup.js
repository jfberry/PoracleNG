const fs = require('fs')
const path = require('path')
const { getConfigDir } = require('../../configResolver')

const BACKUP_DIR = path.resolve(getConfigDir(), '../backups')
const VALID_NAME = /^[a-zA-Z0-9_-]+$/

function sanitizeName(name) {
	if (!name || !VALID_NAME.test(name)) return null
	return name
}

exports.run = async (client, msg, args, options) => {
	try {
		// Check target
		if (!msg.isFromAdmin) {
			client.log.info(`${msg.userId} ran "backup" command`)
			return await msg.react('🙅')
		}

		const util = client.createUtil(msg, options)

		const {
			canContinue, target, currentProfileNo,
		} = await util.buildTarget(args)

		if (!canContinue) return
		client.log.info(`${target.name}/${target.type}-${target.id}: ${__filename.slice(__dirname.length + 1, -3)} ${args}`)

		if (args.includes('remove')) {
			args.splice(args.indexOf('remove'), 1)
			const name = sanitizeName(args[0])
			if (!name) return msg.reply(client.translator.translate('Include a valid backup name (letters, numbers, hyphens, underscores only)'))
			const filePath = path.join(BACKUP_DIR, `${name}.json`)
			if (fs.existsSync(filePath)) {
				fs.unlinkSync(filePath)
				return msg.react(client.translator.translate('✅'))
			}
			return msg.react(client.translator.translate('👌'))
		}

		const name = sanitizeName(args[0])
		if (!name) return msg.reply(client.translator.translate('Your backup needs a valid name (letters, numbers, hyphens, underscores only)'))
		if (args.includes('list')) return msg.reply(client.translator.translate(`To list existing backups, run \`${util.prefix}restore list\``))
		const backup = {
			monsters: await client.query.selectAllQuery('monsters', { id: target.id, profile_no: currentProfileNo }),
			raid: await client.query.selectAllQuery('raid', { id: target.id, profile_no: currentProfileNo }),
			egg: await client.query.selectAllQuery('egg', { id: target.id, profile_no: currentProfileNo }),
			quest: await client.query.selectAllQuery('quest', { id: target.id, profile_no: currentProfileNo }),
			invasion: await client.query.selectAllQuery('invasion', { id: target.id, profile_no: currentProfileNo }),
			weather: await client.query.selectAllQuery('weather', { id: target.id, profile_no: currentProfileNo }),
			lures: await client.query.selectAllQuery('lures', { id: target.id, profile_no: currentProfileNo }),
			gym: await client.query.selectAllQuery('gym', { id: target.id, profile_no: currentProfileNo }),
			forts: await client.query.selectAllQuery('forts', { id: target.id, profile_no: currentProfileNo }),
			nests: await client.query.selectAllQuery('nests', { id: target.id, profile_no: currentProfileNo }),
			maxbattle: await client.query.selectAllQuery('maxbattle', { id: target.id, profile_no: currentProfileNo }),
		}
		for (const rows of Object.values(backup)) {
			rows.forEach((x) => { x.id = 0; delete x.uid; x.profile_no = 0 })
		}

		fs.mkdirSync(BACKUP_DIR, { recursive: true })
		fs.writeFileSync(path.join(BACKUP_DIR, `${name}.json`), JSON.stringify(backup, null, '\t'))
		msg.react(client.translator.translate('✅'))
	} catch (err) {
		client.log.error('Backup command unhappy:', err, err.source, err.error)
	}
}
