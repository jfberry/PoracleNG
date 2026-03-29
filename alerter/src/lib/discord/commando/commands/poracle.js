const axios = require('axios')
const communityLogic = require('../../../communityLogic')

exports.run = async (client, msg) => {
	try {
		let communityToAdd

		if (client.config.areaSecurity.enabled) {
			for (const community of Object.keys(client.config.areaSecurity.communities)) {
				if (client.config.areaSecurity.communities[community].discord.channels.includes(msg.channel.id)) {
					communityToAdd = community
					break
				}
			}
			if (!communityToAdd) {
				return client.logs.log.info(`${msg.author.username} tried to register in ${msg.channel.name}`)
			}
		} else if (!client.config.discord.channels.includes(msg.channel.id)) {
			return client.logs.log.info(`${msg.author.username} tried to register in ${msg.channel.name}`)
		}

		const command = msg.content.split(' ')[0].substring(1)

		let language = ''

		if (client.config.general.availableLanguages) {
			for (const [key, availableLanguage] of Object.entries(client.config.general.availableLanguages)) {
				if (availableLanguage.poracle === command) {
					language = key
					break
				}
			}
		}

		const user = await client.query.selectOneQuery('humans', { id: msg.author.id })

		if (user) {
			if (user.admin_disable && !user.disabled_date) {
				return await msg.react('🙅') // account was disabled by admin, don't let him re-enable
			}

			const update = {}
			let updateRequired = false

			if (!user.enabled) {
				update.enabled = 1
				update.fails = 0
				updateRequired = true
			}

			if (client.config.general.roleCheckMode === 'disable-user') {
				if (user.admin_disable && user.disabled_date) {
					update.admin_disable = 0
					update.disabled_date = null

					updateRequired = true
					client.logs.discord.log({
						level: 'debug',
						message: `user ${msg.author.username} used poracle command to remove admin_disable flag`,
						event: 'discord:registerCheck',
					})
				}
			}

			if (communityToAdd) {
				update.community_membership = JSON.stringify(communityLogic.addCommunity(client.config, user.community_membership ? JSON.parse(user.community_membership) : [], communityToAdd))
				update.area_restriction = JSON.stringify(communityLogic.calculateLocationRestrictions(
					client.config,
					JSON.parse(update.community_membership),
				))
				updateRequired = true
			}

			if (updateRequired) {
				await client.query.updateQuery('humans', update, { id: msg.author.id.toString() })
				await msg.react('✅')
			} else {
				await msg.react('👌')
			}

			//			await client.query.updateQuery('humans', { language: language }, { id: msg.author.id })
		} else {
			await client.query.insertQuery('humans', {
				id: msg.author.id,
				type: 'discord:user',
				name: client.emojiStrip(msg.author.username),
				area: '[]',
				language,
				community_membership: communityToAdd ? JSON.stringify([communityToAdd.toLowerCase()]) : '[]',
				area_restriction: communityToAdd ? JSON.stringify(communityLogic.calculateLocationRestrictions(client.config, [communityToAdd])) : null,
			})
			await msg.react('✅')
		}

		client.logs.log.info(`${client.emojiStrip(msg.author.username)} Registered!`)

		try {
			const resp = await axios.post(`${client.config.processor.url}/api/dts/render`, {
				type: 'greeting',
				platform: 'discord',
				language,
				view: { prefix: client.config.discord.prefix },
			}, {
				headers: { 'Content-Type': 'application/json', ...client.config.processor.headers },
				timeout: 5000,
			})
			if (resp.data.status === 'ok' && resp.data.message) {
				const discordMsgToSend = resp.data.message
				if (discordMsgToSend.embed) {
					discordMsgToSend.embeds = [discordMsgToSend.embed]
					delete discordMsgToSend.embed
				}
				await msg.author.send(discordMsgToSend)
			}
		} catch (err) {
			client.logs.log.warn(`Greeting render failed: ${err.message}`)
		}
	} catch (err) {
		client.logs.log.error('!poracle command errored with:', err)
	}
}
