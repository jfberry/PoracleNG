package rowtext

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// QuestRowText generates a human-readable description of a quest tracking rule.
func (g *Generator) QuestRowText(tr *i18n.Translator, quest *db.QuestTracking) string {
	rewardThing := g.rewardText(tr, quest.RewardType, quest.Reward, quest.Form)

	s := tr.Tf("tracking.reward_fmt", rewardThing)

	if quest.Amount > 0 {
		s += " " + tr.Tf("tracking.reward_min_fmt", quest.Amount)
	}

	if quest.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", quest.Distance)
	}

	s += " " + standardText(tr, quest.Template, g.DefaultTemplateName, quest.Clean, false)
	s = appendOverride(tr, s, quest.OverrideLocationLabel, quest.OverrideAreas)

	return s
}

// rewardText describes a quest-style reward (reward_type / reward / form),
// shared by quest and tappable rules. Stardust and pokecoin hold their
// minimum amount in reward; the other types hold an item or pokemon id with
// 0 as "any".
func (g *Generator) rewardText(tr *i18n.Translator, rewardType, reward, form int) string {
	var rewardThing string

	switch rewardType {
	case 7:
		// Pokemon reward (reward==0 is the "all pokemon" wildcard)
		if reward == 0 {
			rewardThing = tr.T("tracking.reward_any_pokemon")
			break
		}
		mon := g.GD.GetMonster(reward, form)
		if mon != nil {
			rewardThing = tr.T(gamedata.PokemonTranslationKey(reward))
			if form != 0 {
				formName := tr.T(gamedata.FormTranslationKey(form))
				if formName != gamedata.FormTranslationKey(form) {
					rewardThing += " " + formName
				}
			}
		} else {
			rewardThing = fmt.Sprintf("%s %d %d", tr.T("tracking.unknown_monster"), reward, form)
		}

	case 3:
		// Stardust reward
		if reward > 0 {
			rewardThing = tr.Tf("tracking.reward_stardust_min_fmt", reward)
		} else {
			rewardThing = tr.T("tracking.reward_stardust")
		}

	case 8:
		// Pokecoins reward
		if reward > 0 {
			rewardThing = tr.Tf("tracking.reward_pokecoin_min_fmt", reward)
		} else {
			rewardThing = tr.T("tracking.reward_pokecoin")
		}

	case 2:
		// Item reward (reward==0 is the "all items" wildcard)
		if reward == 0 {
			rewardThing = tr.T("tracking.reward_any_item")
			break
		}
		item := g.GD.GetItem(reward)
		if item != nil {
			rewardThing = tr.T(gamedata.ItemTranslationKey(reward))
		} else {
			rewardThing = fmt.Sprintf("%s %d", tr.T("tracking.unknown_item"), reward)
		}

	case 12:
		// Mega energy reward
		if reward == 0 {
			rewardThing = tr.T("tracking.reward_mega_energy")
		} else {
			mon := g.GD.GetMonster(reward, 0)
			var monsterName string
			if mon != nil {
				monsterName = tr.T(gamedata.PokemonTranslationKey(reward))
			} else {
				monsterName = fmt.Sprintf("%s %d", tr.T("tracking.unknown_monster"), reward)
			}
			rewardThing = tr.Tf("tracking.reward_mega_energy_fmt", monsterName)
		}

	case 4:
		// Candy reward
		if reward == 0 {
			rewardThing = tr.T("tracking.reward_candy")
		} else {
			mon := g.GD.GetMonster(reward, 0)
			var monsterName string
			if mon != nil {
				monsterName = tr.T(gamedata.PokemonTranslationKey(reward))
			} else {
				monsterName = fmt.Sprintf("%s %d", tr.T("tracking.unknown_monster"), reward)
			}
			rewardThing = tr.Tf("tracking.reward_candy_fmt", monsterName)
		}

	case 9:
		// XL candy reward (tappables; quests don't track it)
		if reward == 0 {
			rewardThing = tr.T("tracking.reward_xl_candy")
		} else {
			var monsterName string
			if g.GD.GetMonster(reward, 0) != nil {
				monsterName = tr.T(gamedata.PokemonTranslationKey(reward))
			} else {
				monsterName = fmt.Sprintf("%s %d", tr.T("tracking.unknown_monster"), reward)
			}
			rewardThing = tr.Tf("tracking.reward_xl_candy_fmt", monsterName)
		}
	}

	return rewardThing
}
