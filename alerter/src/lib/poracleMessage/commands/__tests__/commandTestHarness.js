const regexFactory = require('../../../../util/regex')

function createMockTranslatorFactory() {
	const factory = {
		config: {
			general: {
				locale: 'en',
				availableLanguages: { en: 'English' },
			},
			locale: { language: 'en' },
		},
		translators: {},
		commandTranslators: null,
		Translator(locale) {
			if (!this.translators[locale]) {
				this.translators[locale] = {
					translate: (bit) => bit,
					translateFormat: (bit, ...args) => {
						let str = bit
						let i = args.length
						while (i--) {
							str = str.replace(new RegExp(`\\{${i}\\}`, 'gm'), args[i])
						}
						return str
					},
					reverse: (bit) => bit,
				}
			}
			return this.translators[locale]
		},
		get default() { return this.Translator('en') },
		get CommandTranslators() {
			if (this.commandTranslators) return this.commandTranslators
			this.commandTranslators = [this.Translator('en')]
			return this.commandTranslators
		},
		reverseTranslateCommand(key) { return key },
		translateCommand(key) { return [key] },
	}
	return factory
}

function createMockClient(overrides = {}) {
	const translatorFactory = overrides.translatorFactory || createMockTranslatorFactory()
	const re = regexFactory(translatorFactory)

	const queries = { inserts: [], deletes: [], selects: [] }

	const client = {
		re,
		translatorFactory,
		translator: translatorFactory.Translator('en'),
		GameData: overrides.GameData || {
			monsters: {},
			moves: {},
			items: {},
			grunts: {},
			utilData: {
				types: {},
				teams: {
					0: { name: 'Harmony' },
					1: { name: 'Mystic' },
					2: { name: 'Valor' },
					3: { name: 'Instinct' },
					4: { name: 'All' },
				},
				lures: {
					501: { name: 'Normal' },
					502: { name: 'Glacial' },
					503: { name: 'Mossy' },
					504: { name: 'Magnetic' },
					505: { name: 'Rainy' },
					506: { name: 'Sparkly' },
				},
				raidLevels: {
					1: 'Normal', 3: 'Rare', 5: 'Legendary', 6: 'Mega', 7: 'Ultra Beast', 8: 'Primal',
				},
				maxbattleLevels: {
					1: '1 Star', 2: '2 Star', 3: '3 Star', 4: '4 Star', 5: '5 Star', 6: '6 Star',
				},
				genData: {},
				rarity: {},
				size: {},
				pokestopEvent: {},
			},
		},
		config: overrides.config || {
			general: {
				locale: 'en',
				defaultTemplateName: '1',
				availableLanguages: { en: 'English' },
			},
			tracking: {
				defaultDistance: 0,
				maxDistance: 0,
				everythingFlagPermissions: 'allow-any',
				defaultUserTrackingLevelCap: 0,
				enableGymBattle: false,
			},
			pvp: {
				pvpFilterMaxRank: 4096,
				pvpFilterLittleMinCP: 0,
				pvpFilterGreatMinCP: 0,
				pvpFilterUltraMinCP: 0,
				levelCaps: [50],
			},
			discord: {
				prefix: '!',
				admins: ['test-user'],
			},
			database: {
				client: 'mysql',
			},
		},
		query: {
			selectAllQuery: async (table, where) => {
				queries.selects.push({ table, where })
				return overrides.existingTracked || []
			},
			insertQuery: async (table, rows) => {
				queries.inserts.push({ table, rows })
			},
			deleteWhereInQuery: async (table, where, values, column) => {
				queries.deletes.push({
					table, where, values, column,
				})
				return values.length
			},
			deleteQuery: async (table, where) => {
				queries.deletes.push({ table, where })
				return 1
			},
			mysteryQuery: async (sql) => {
				queries.deletes.push({ sql })
				return { affectedRows: 1 }
			},
		},
		log: {
			info: () => {},
			warn: () => {},
			error: () => {},
			debug: () => {},
		},
		// eslint-disable-next-line no-unused-vars
		createUtil: (msg, options) => ({
			prefix: '!',
			client,
			buildTarget: async () => ({
				canContinue: true,
				target: {
					id: 'test-user', type: 'discord:user', name: 'TestUser', webhook: false,
				},
				userHasLocation: true,
				userHasArea: true,
				language: 'en',
				currentProfileNo: 1,
			}),
			commandAllowed: async () => true,
		}),
		updatedDiff: (existing, toInsert) => {
			const diff = { uid: true }
			for (const key of Object.keys(toInsert)) {
				if (existing[key] !== undefined && existing[key] !== toInsert[key]) {
					diff[key] = true
				}
			}
			return diff
		},
		triggerReloadAlerts: () => {},
		scannerQuery: null,
	}

	return { client, queries }
}

function createMockMsg() {
	const replies = []
	const reactions = []

	const msg = {
		replies,
		reactions,
		reply: async (text, options) => { replies.push({ text, options }) },
		react: async (emoji) => { reactions.push(emoji) },
		getPings: () => '',
		isFromAdmin: true,
		isDM: false,
		userId: 'test-user',
		msg: {
			author: { id: 'test-user', username: 'TestUser' },
			channel: {
				id: 'test-channel',
				isDMBased: () => false,
				isTextBased: () => true,
			},
		},
	}

	return msg
}

async function runCommand(commandFile, args, overrides = {}) {
	const { client, queries } = createMockClient(overrides)
	const msg = createMockMsg()
	const command = require(`../${commandFile}`)
	await command.run(client, msg, [...args], {})
	return {
		replies: msg.replies,
		reactions: msg.reactions,
		queries,
	}
}

module.exports = { runCommand, createMockClient, createMockMsg }
