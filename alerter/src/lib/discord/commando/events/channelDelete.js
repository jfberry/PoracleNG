module.exports = async (client, channel) => {
	try {
		const isRegistered = await client.query.countQuery('humans', { id: channel.id })
		if (isRegistered) {
			await client.query.deleteHuman({ id: channel.id })
			client.logs.discord.log({
				level: 'debug',
				message: `text channel ${channel.name} was deleted and unregistered`,
				event: 'discord:registerCheck',
			})
		}
	} catch (e) {
		client.logs.discord.error('Discord event channelDelete - Was unable to remove human', e)
	}
}
