#!/usr/bin/env node
/**
 * Generates expected enrichment values from the JS GameData for comparison
 * with Go enricher output.
 *
 * Reads webhook log entries, runs the JS enrichment logic, and writes
 * expected results JSON. Seeks diversity: encountered/unencountered, PVP,
 * mega evolutions, disguises, various sizes, gen exceptions, etc.
 *
 * Usage: node generate_expected.js [logfile] [count]
 */

const fs = require('fs')
const path = require('path')
const readline = require('readline')

const RESOURCES_DATA = path.resolve(__dirname, '../../../../resources/data')
const UTIL_PATH = path.resolve(RESOURCES_DATA, 'util.json')

const GameData = { utilData: JSON.parse(fs.readFileSync(UTIL_PATH)) }
for (const file of ['monsters', 'moves', 'items', 'grunts', 'questTypes', 'types']) {
	try {
		GameData[file] = JSON.parse(fs.readFileSync(path.join(RESOURCES_DATA, `${file}.json`)))
	} catch (e) {
		console.error(`Could not load ${file}.json: ${e.message}`)
		process.exit(1)
	}
}

const defaultIvColors = ['#9D9D9D', '#FFFFFF', '#1EFF00', '#0070DD', '#A335EE', '#FF8000']

function findIvColor(iv) {
	if (iv < 25) return defaultIvColors[0]
	if (iv < 50) return defaultIvColors[1]
	if (iv < 82) return defaultIvColors[2]
	if (iv < 90) return defaultIvColors[3]
	if (iv < 100) return defaultIvColors[4]
	return defaultIvColors[5]
}

function computeGeneration(pokemonId, form) {
	const key = `${pokemonId}_${form}`
	if (GameData.utilData.genException[key]) {
		return parseInt(GameData.utilData.genException[key], 10)
	}
	const entry = Object.entries(GameData.utilData.genData)
		.find(([, genData]) => pokemonId >= genData.min && pokemonId <= genData.max)
	return entry ? parseInt(entry[0], 10) : 0
}

function computeWeaknesses(typeNames) {
	const weaknesses = {}
	for (const typeName of typeNames) {
		const typeInfo = GameData.types[typeName]
		if (!typeInfo) continue
		for (const w of (typeInfo.weaknesses || [])) { if (!weaknesses[w.typeName]) weaknesses[w.typeName] = 1; weaknesses[w.typeName] *= 2 }
		for (const r of (typeInfo.resistances || [])) { if (!weaknesses[r.typeName]) weaknesses[r.typeName] = 1; weaknesses[r.typeName] *= 0.5 }
		for (const i of (typeInfo.immunes || [])) { if (!weaknesses[i.typeName]) weaknesses[i.typeName] = 1; weaknesses[i.typeName] *= 0.25 }
	}
	const result = {}
	for (const [name, mult] of Object.entries(weaknesses)) {
		if (mult === 1) continue
		if (!result[mult]) result[mult] = []
		result[mult].push(name)
	}
	return result
}

function getAlteringWeathers(typeIds, boostStatus) {
	const boostingWeathers = [...new Set(typeIds.map((type) =>
		parseInt(Object.keys(GameData.utilData.weatherTypeBoost)
			.find((key) => GameData.utilData.weatherTypeBoost[key].includes(type)), 10))
		.filter((w) => !isNaN(w)))]
	const nonBoosting = [1, 2, 3, 4, 5, 6, 7].filter((w) => !boostingWeathers.includes(w))
	return boostStatus > 0 ? nonBoosting : boostingWeathers
}

function computeSeenType(msg) {
	const encountered = msg.individual_attack !== null && msg.individual_attack !== undefined
	if (msg.seen_type) {
		switch (msg.seen_type) {
			case 'nearby_stop': return 'pokestop'
			case 'nearby_cell': return 'cell'
			case 'lure': case 'lure_wild': return 'lure'
			case 'lure_encounter': case 'encounter': case 'wild': return msg.seen_type
			default: return ''
		}
	}
	if (msg.pokestop_id === 'None' && msg.spawnpoint_id === 'None') return 'cell'
	if (msg.pokestop_id === 'None') return encountered ? 'encounter' : 'wild'
	return 'pokestop'
}

function collectEvolutions(monster) {
	const evolutions = []
	const megaEvolutions = []
	let count = 0
	const walk = (mon) => {
		if (++count >= 10) return
		if (mon.evolutions) {
			for (const evo of mon.evolutions) {
				const evoMon = GameData.monsters[`${evo.evoId}_${evo.id}`]
				if (!evoMon) continue
				const formName = evoMon.form.name
				const formNormalised = (formName === 'Normal' || formName === '') ? '' : formName
				evolutions.push({
					id: evo.evoId, form: evo.id, nameEng: evoMon.name,
					formNormalisedEng: formNormalised,
					fullNameEng: evoMon.name + (formNormalised ? ' ' + formNormalised : ''),
					typeIds: evoMon.types.map((t) => t.id),
					baseAttack: evoMon.stats.baseAttack, baseDefense: evoMon.stats.baseDefense, baseStamina: evoMon.stats.baseStamina,
				})
				walk(evoMon)
			}
		}
		if (mon.tempEvolutions) {
			for (const te of mon.tempEvolutions) {
				const pattern = GameData.utilData.megaName[te.tempEvoId] || '{0}'
				megaEvolutions.push({
					evolution: te.tempEvoId,
					fullNameEng: pattern.replace('{0}', mon.name),
					baseAttack: te.stats?.baseAttack || 0,
					baseDefense: te.stats?.baseDefense || 0,
					baseStamina: te.stats?.baseStamina || 0,
				})
			}
		}
	}
	walk(monster)
	return { evolutions, megaEvolutions }
}

function enrichPvpRankings(msg) {
	if (!msg.pvp) return {}
	const result = {}
	for (const [league, entries] of Object.entries(msg.pvp)) {
		result[league] = entries.filter((e) => e.rank > 0).map((rank) => {
			const formId = rank.form || 0
			const mon = GameData.monsters[`${rank.pokemon}_${formId}`] || GameData.monsters[`${rank.pokemon}_0`]
			const entry = {
				rank: rank.rank, cp: rank.cp, level: rank.level, cap: rank.cap, capped: rank.capped,
				pokemon: rank.pokemon, form: rank.form, evolution: rank.evolution, percentage: rank.percentage,
			}
			entry.percentageFormatted = rank.percentage <= 1 ? (rank.percentage * 100).toFixed(2) : rank.percentage.toFixed(2)
			if (rank.cap && !rank.capped) {
				entry.levelWithCap = `${rank.level}/${rank.cap}`
			} else {
				entry.levelWithCap = rank.level
			}
			if (mon) {
				entry.nameEng = mon.name
				const formName = mon.form.name
				entry.formNormalisedEng = (formName === 'Normal' || formName === '') ? '' : formName
				if (rank.evolution) {
					const pattern = GameData.utilData.megaName[rank.evolution] || '{0}'
					entry.fullNameEng = pattern.replace('{0}', mon.name + (entry.formNormalisedEng ? ' ' + entry.formNormalisedEng : ''))
				} else {
					entry.fullNameEng = mon.name + (entry.formNormalisedEng ? ' ' + entry.formNormalisedEng : '')
				}
				entry.baseAttack = mon.stats.baseAttack
				entry.baseDefense = mon.stats.baseDefense
				entry.baseStamina = mon.stats.baseStamina
			} else {
				entry.nameEng = `Pokemon ${rank.pokemon}`
				entry.fullNameEng = `Pokemon ${rank.pokemon}`
				entry.baseAttack = 0; entry.baseDefense = 0; entry.baseStamina = 0
			}
			return entry
		})
	}
	return result
}

function processPokemon(msg) {
	const form = msg.form ?? 0
	const monster = GameData.monsters[`${msg.pokemon_id}_${form}`] || GameData.monsters[`${msg.pokemon_id}_0`]
	if (!monster) return null

	const encountered = msg.individual_attack !== null && msg.individual_attack !== undefined
	const weather = msg.boosted_weather || msg.weather || 0
	const typeIds = monster.types.map((t) => t.id)
	const typeNames = monster.types.map((t) => t.name)
	const gen = computeGeneration(msg.pokemon_id, form)
	const genInfo = GameData.utilData.genData[gen]
	const iv = encountered ? ((msg.individual_attack + msg.individual_defense + msg.individual_stamina) / 0.45) : -1
	const { evolutions, megaEvolutions } = collectEvolutions(monster)
	const pvpEnriched = enrichPvpRankings(msg)

	return {
		pokemonId: msg.pokemon_id, form,
		nameEng: monster.name, formNameEng: monster.form.name,
		formNormalisedEng: (monster.form.name === 'Normal' || monster.form.name === '') ? '' : monster.form.name,
		fullNameEng: monster.name + (((monster.form.name === 'Normal' || monster.form.name === '') ? '' : monster.form.name) ? ' ' + monster.form.name : ''),
		typeIds, typeNames,
		typeColor: GameData.utilData.types[typeNames[0]]?.color || '',
		typeEmojiKeys: typeNames.map((n) => GameData.utilData.types[n]?.emoji || ''),
		generation: gen, generationName: genInfo?.name || '', generationRoman: genInfo?.roman || '',
		baseAttack: monster.stats.baseAttack, baseDefense: monster.stats.baseDefense, baseStamina: monster.stats.baseStamina,
		encountered,
		iv: iv === -1 ? -1 : parseFloat(iv.toFixed(2)),
		ivColor: encountered ? findIvColor(iv) : '',
		atk: encountered ? msg.individual_attack : 0,
		def: encountered ? msg.individual_defense : 0,
		sta: encountered ? msg.individual_stamina : 0,
		cp: encountered ? msg.cp : 0,
		level: encountered ? msg.pokemon_level : 0,
		catchBase: encountered && msg.base_catch ? parseFloat((msg.base_catch * 100).toFixed(2)) : 0,
		catchGreat: encountered && msg.great_catch ? parseFloat((msg.great_catch * 100).toFixed(2)) : 0,
		catchUltra: encountered && msg.ultra_catch ? parseFloat((msg.ultra_catch * 100).toFixed(2)) : 0,
		weather,
		boostingWeathers: [...new Set(typeIds.map((type) =>
			parseInt(Object.keys(GameData.utilData.weatherTypeBoost).find((key) => GameData.utilData.weatherTypeBoost[key].includes(type)), 10))
			.filter((w) => !isNaN(w)))],
		alteringWeathers: getAlteringWeathers(typeIds, weather),
		weaknesses: computeWeaknesses(typeNames),
		quickMoveId: encountered ? msg.move_1 : 0, chargeMoveId: encountered ? msg.move_2 : 0,
		quickMoveNameEng: encountered && GameData.moves[msg.move_1] ? GameData.moves[msg.move_1].name : '',
		chargeMoveNameEng: encountered && GameData.moves[msg.move_2] ? GameData.moves[msg.move_2].name : '',
		quickMoveType: encountered && GameData.moves[msg.move_1] ? GameData.moves[msg.move_1].type : '',
		chargeMoveType: encountered && GameData.moves[msg.move_2] ? GameData.moves[msg.move_2].type : '',
		genderName: GameData.utilData.genders[msg.gender]?.name || '',
		genderEmojiKey: GameData.utilData.genders[msg.gender]?.emoji || '',
		sizeName: msg.size && GameData.utilData.size[msg.size] ? GameData.utilData.size[msg.size] : '',
		seenType: computeSeenType(msg),
		hasEvolutions: !!(monster.evolutions && monster.evolutions.length),
		hasMegaEvolutions: !!(monster.tempEvolutions && monster.tempEvolutions.length),
		evolutions, megaEvolutions,
		googleMapUrl: `https://maps.google.com/maps?q=${msg.latitude},${msg.longitude}`,
		appleMapUrl: `https://maps.apple.com/place?coordinate=${msg.latitude},${msg.longitude}`,
		hasPvp: !!msg.pvp,
		pvpEnriched,
		hasDisguise: !!(msg.display_pokemon_id && msg.display_pokemon_id !== msg.pokemon_id),
		disguisePokemonId: (msg.display_pokemon_id && msg.display_pokemon_id !== msg.pokemon_id) ? msg.display_pokemon_id : 0,
		disguiseForm: (msg.display_pokemon_id && msg.display_pokemon_id !== msg.pokemon_id) ? (msg.display_form ?? 0) : 0,
	}
}

function processRaid(msg) {
	const result = {
		gymId: msg.gym_id, level: msg.level, teamId: msg.team_id ?? 0,
		gymColor: GameData.utilData.teams[msg.team_id ?? 0]?.color || '',
		levelNameEng: GameData.utilData.raidLevels[msg.level] || '',
	}
	if (msg.pokemon_id > 0) {
		const form = msg.form ?? 0
		const monster = GameData.monsters[`${msg.pokemon_id}_${form}`] || GameData.monsters[`${msg.pokemon_id}_0`]
		if (monster) {
			const typeIds = monster.types.map((t) => t.id)
			const typeNames = monster.types.map((t) => t.name)
			Object.assign(result, {
				pokemonId: msg.pokemon_id, form,
				nameEng: monster.name, formNameEng: monster.form.name,
				typeIds, typeNames,
				baseAttack: monster.stats.baseAttack, baseDefense: monster.stats.baseDefense, baseStamina: monster.stats.baseStamina,
				weaknesses: computeWeaknesses(typeNames),
				generation: computeGeneration(msg.pokemon_id, form),
				generationRoman: GameData.utilData.genData[computeGeneration(msg.pokemon_id, form)]?.roman || '',
			})
		}
	}
	return result
}

async function main() {
	const logFile = process.argv[2] || path.resolve(__dirname, '../../../../logs/webhooks.log')
	const maxPerType = parseInt(process.argv[3] || '50', 10)

	// Track diversity targets
	const pokemonNeeds = { encountered: 15, unencountered: 5, pvp: 10, boosted: 5, mega: 3, disguise: 3 }
	const pokemonHas = { encountered: 0, unencountered: 0, pvp: 0, boosted: 0, mega: 0, disguise: 0, total: 0 }
	const raidCount = { total: 0 }
	const results = []

	const rl = readline.createInterface({ input: fs.createReadStream(logFile) })

	for await (const line of rl) {
		let entry
		try { entry = JSON.parse(line) } catch { continue }

		if (entry.type === 'pokemon' && pokemonHas.total < maxPerType) {
			const msg = entry.message
			const encountered = msg.individual_attack !== null && msg.individual_attack !== undefined
			const hasPvp = !!msg.pvp
			const weather = msg.boosted_weather || msg.weather || 0
			const form = msg.form ?? 0
			const monster = GameData.monsters[`${msg.pokemon_id}_${form}`] || GameData.monsters[`${msg.pokemon_id}_0`]
			const hasMega = !!(monster && monster.tempEvolutions && monster.tempEvolutions.length)
			const hasDisguise = !!(msg.display_pokemon_id && msg.display_pokemon_id !== msg.pokemon_id)

			// Prioritize diverse cases
			let dominated = false
			if (encountered && pokemonHas.encountered >= pokemonNeeds.encountered) dominated = true
			if (!encountered && pokemonHas.unencountered >= pokemonNeeds.unencountered) dominated = true

			// Always take PVP, mega, disguise, boosted if we need them
			let priority = false
			if (hasPvp && pokemonHas.pvp < pokemonNeeds.pvp) priority = true
			if (hasMega && pokemonHas.mega < pokemonNeeds.mega) priority = true
			if (hasDisguise && pokemonHas.disguise < pokemonNeeds.disguise) priority = true
			if (weather > 0 && pokemonHas.boosted < pokemonNeeds.boosted) priority = true

			if (dominated && !priority) continue

			const expected = processPokemon(msg)
			if (expected) {
				results.push({ type: 'pokemon', message: msg, expected })
				pokemonHas.total++
				if (encountered) pokemonHas.encountered++
				else pokemonHas.unencountered++
				if (hasPvp) pokemonHas.pvp++
				if (weather > 0) pokemonHas.boosted++
				if (hasMega) pokemonHas.mega++
				if (hasDisguise) pokemonHas.disguise++
			}
		} else if (entry.type === 'raid' && raidCount.total < maxPerType) {
			const expected = processRaid(entry.message)
			if (expected) {
				results.push({ type: 'raid', message: entry.message, expected })
				raidCount.total++
			}
		}
	}

	const outPath = path.resolve(__dirname, 'expected.json')
	fs.writeFileSync(outPath, JSON.stringify(results, null, 2))
	console.log(`Wrote ${results.length} test cases to ${outPath}`)
	console.log(`  Pokemon: ${pokemonHas.total} (enc:${pokemonHas.encountered} unenc:${pokemonHas.unencountered} pvp:${pokemonHas.pvp} boost:${pokemonHas.boosted} mega:${pokemonHas.mega} disguise:${pokemonHas.disguise})`)
	console.log(`  Raids: ${raidCount.total}`)
}

main().catch((err) => { console.error(err); process.exit(1) })
