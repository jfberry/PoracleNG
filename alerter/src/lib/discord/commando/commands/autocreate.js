const { ChannelType, PermissionFlagsBits } = require('discord.js')
const PoracleDiscordMessage = require('../../poracleDiscordMessage')
const PoracleDiscordState = require('../../poracleDiscordState')
const { loadConfigJson } = require('../../../configResolver')

function format(str, args) {
	let newStr = str
	let i = args.length
	while (i--) {
		newStr = newStr.replace(new RegExp(`\\{${i}\\}`, 'gm'), args[i])
	}
	return newStr
}

const rolePermissionMap = {
	view: PermissionFlagsBits.ViewChannel,
	viewHistory: PermissionFlagsBits.ReadMessageHistory,
	send: PermissionFlagsBits.SendMessages,
	react: PermissionFlagsBits.AddReactions,
	pingEveryone: PermissionFlagsBits.MentionEveryone,
	embedLinks: PermissionFlagsBits.EmbedLinks,
	attachFiles: PermissionFlagsBits.AttachFiles,
	sendTTS: PermissionFlagsBits.SendTTSMessages,
	externalEmoji: PermissionFlagsBits.UseExternalEmojis,
	externalStickers: PermissionFlagsBits.UseExternalStickers,
	createPublicThreads: PermissionFlagsBits.CreatePublicThreads,
	createPrivateThreads: PermissionFlagsBits.CreatePrivateThreads,
	sendThreads: PermissionFlagsBits.SendMessagesInThreads,
	slashCommands: PermissionFlagsBits.UseApplicationCommands,
	connect: PermissionFlagsBits.Connect,
	speak: PermissionFlagsBits.Speak,
	autoMic: PermissionFlagsBits.UseVAD,
	stream: PermissionFlagsBits.Stream,
	vcActivities: PermissionFlagsBits.UseEmbeddedActivities,
	prioritySpeaker: PermissionFlagsBits.PrioritySpeaker,
	createInvite: PermissionFlagsBits.CreateInstantInvite,
	channels: PermissionFlagsBits.ManageChannels,
	messages: PermissionFlagsBits.ManageMessages,
	roles: PermissionFlagsBits.ManageRoles,
	webhooks: PermissionFlagsBits.ManageWebhooks,
	threads: PermissionFlagsBits.ManageThreads,
	events: PermissionFlagsBits.ManageEvents,
	mute: PermissionFlagsBits.MuteMembers,
	deafen: PermissionFlagsBits.DeafenMembers,
	move: PermissionFlagsBits.MoveMembers,
}

function addRolePermissions(role, allowed, deny) {
	for (const [key, permissionFlag] of Object.entries(rolePermissionMap)) {
		if (role[key] === true) {
			allowed.push(permissionFlag)
		} else if (role[key] === false) {
			deny.push(permissionFlag)
		}
	}
}

exports.run = async (client, msg, [args]) => {
	try {
		if (!client.config.discord.admins.includes(msg.author.id)) return

		// Check target
		if (!client.config.discord.admins.includes(msg.author.id) && !msg.channel.isDMBased()) {
			return await msg.author.send(client.translator.translate('Please run commands in Direct Messages'))
		}

		for (const commandText of msg.content.split('\n')) {
			args = commandText.slice(client.config.discord.prefix.length)
				.trim()
				.match(/(".*?"|[^"\s]+)+(?=\s*|\s*$)/g)
				.map((x) => x.replace(/"/g, ''))
			args.shift()
		}

		let { guild } = msg

		let guildIdOverride = args.find((arg) => arg.match(client.re.guildRe))
		if (guildIdOverride) [, , guildIdOverride] = guildIdOverride.match(client.re.guildRe)

		if (guildIdOverride) {
			try {
				guild = await msg.client.guilds.fetch(guildIdOverride)
			} catch {
				return await msg.reply('I was not able to retrieve that guild')
			}
		}

		if (!guild) {
			return await msg.reply('No guild has been set, either execute inside a channel or specify guild<id>')
		}

		if (!guild.members.me.permissions.has(PermissionFlagsBits.ManageWebhooks)) {
			return await msg.reply('I have not been allowed to manage webhooks!')
		}
		if (!guild.members.me.permissions.has(PermissionFlagsBits.ManageChannels)) {
			return await msg.reply('I have not been allowed to manage channels!')
		}

		// Remove arguments that we don't want to keep
		for (let i = args.length - 1; i >= 0; i--) {
			if (args[i].match(client.re.guildRe)) args.splice(i, 1)
		}

		const channelTemplate = loadConfigJson('channelTemplate.json')
		if (!channelTemplate) {
			return await msg.reply('No channel templates defined - create config/channelTemplate.json (see examples/channelTemplate.json)')
		}

		const templateName = args.shift()

		const template = channelTemplate.find((x) => x.name === templateName)
		if (!template || !template.definition) {
			return await msg.reply('I can\'t find that channel template! (remember it has to be your first parameter)')
		}

		// switch underscores back in so works for substitution later
		const subArgs = []
		for (let x = 0; x < args.length; x++) {
			subArgs[x] = args[x].replace(/ /g, '_')
		}

		let categoryId
		if (template.definition.category) {
			const categoryOptions = {
				type: ChannelType.GuildCategory,
			}

			const categoryName = format(template.definition.category.categoryName, args)

			// add role permissions
			let roleId
			if (template.definition.category.roles) {
				const roleOverwrites = []
				for (const role of template.definition.category.roles) {
					const allowed = []
					const deny = []
					const roleNames = guild.roles.cache.map((r) => r.name)
					const roleIds = guild.roles.cache.map((r) => r.id)
					for (let x = 0; x < roleNames.length; x++) {
						if ((format(role.name, args)) === roleNames[x]) {
							roleId = await guild.roles.cache.get(roleIds[x])
						}
					}
					if (!roleId) {
						roleId = await guild.roles.create({
							name: (format(role.name, args)),
							permissions: [],
						})
					}
					addRolePermissions(role, allowed, deny)
					roleOverwrites.push({ id: roleId, allow: allowed, deny })
				}
				categoryOptions.permissionOverwrites = roleOverwrites
			}

			// discord.js v14 expects a single create payload with the channel name included
			const category = await guild.channels.create({
				name: categoryName,
				...categoryOptions,
			})
			await msg.reply(`>> Creating ${categoryName}`)
			categoryId = category.id
		}

		for (const channelDefinition of template.definition.channels) {
			const channelOptions = {}
			if (channelDefinition.channelType === 'text') {
				channelOptions.type = ChannelType.GuildText
			} else if (channelDefinition.channelType === 'voice') {
				channelOptions.type = ChannelType.GuildVoice
			}
			if (categoryId) {
				channelOptions.parent = categoryId
			}

			if (channelDefinition.topic) {
				channelOptions.topic = format(channelDefinition.topic, args)
			}

			const channelName = format(channelDefinition.channelName, args)

			// add role permissions
			let roleId
			if (channelDefinition.roles) {
				const roleOverwrites = []
				for (const role of channelDefinition.roles) {
					const allowed = []
					const deny = []
					const roleNames = guild.roles.cache.map((r) => r.name)
					const roleIds = guild.roles.cache.map((r) => r.id)
					for (let x = 0; x < roleNames.length; x++) {
						if ((format(role.name, args)) === roleNames[x]) {
							roleId = await guild.roles.cache.get(roleIds[x])
						}
					}
					if (!roleId) {
						roleId = await guild.roles.create({
							name: (format(role.name, args)),
							permissions: [],
						})
					}
					addRolePermissions(role, allowed, deny)
					roleOverwrites.push({ id: roleId, allow: allowed, deny })
				}
				channelOptions.permissionOverwrites = roleOverwrites
			}

			// discord.js v14 expects a single create payload with the channel name included
			const channel = await guild.channels.create({
				name: channelName,
				...channelOptions,
			})
			await msg.reply(`>> Creating ${channelName}`)

			// exit loop if simple text channel
			if (!channelDefinition.controlType) {
				// eslint-disable-next-line no-continue
				continue
			}

			const { controlType } = channelDefinition
			await msg.reply(`>> Adding control type: ${controlType}`)

			// register channel in poracle
			let id
			let type
			let name

			if (controlType === 'bot') {
				id = channel.id
				type = 'discord:channel'
				name = format(channelDefinition.channelName, subArgs)
			} else {
				const webhookName = format(channelDefinition.channelName, subArgs)
				const res = await channel.createWebhook({ name: 'Poracle' })
				id = res.url
				type = 'webhook'
				name = channelDefinition.webhookName ? format(channelDefinition.webhookName, subArgs) : webhookName
			}

			// Create
			await client.query.insertQuery('humans', {
				id,
				type,
				name,
				area: '[]',
				community_membership: '[]',
			})

			// Commands

			const commands = channelDefinition.commands.map((x) => format(x, subArgs))

			const pdm = new PoracleDiscordMessage(client, msg)
			const pds = new PoracleDiscordState(client)
			const target = { type, id, name }
			await msg.reply(`>> Executing as ${target.type} / ${target.name} ${target.type !== 'webhook' ? target.id : ''}`)

			for (const commandText of commands) {
				await msg.reply(`>>> Executing ${commandText}`)

				let commandArgs = commandText.trim().split(/ +/g)
				commandArgs = commandArgs.map((arg) => client.translatorFactory.reverseTranslateCommand(arg.toLowerCase().replace(/_/g, ' '), true).toLowerCase())

				const cmdName = commandArgs.shift()

				const cmd = require(`../../../poracleMessage/commands/${cmdName}`)

				await cmd.run(
					pds,
					pdm,
					commandArgs,
					{
						targetOverride: target,
					},
				)
			}
		}
	} catch (err) {
		await msg.reply('Failed to run autocreate, check logs')
		client.logs.log.error(`Autocreate command "${msg.content}" unhappy:`, err)
	}
}
