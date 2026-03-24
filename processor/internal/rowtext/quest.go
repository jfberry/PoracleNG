package rowtext

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// QuestRowText generates a human-readable description of a quest tracking rule.
func (g *Generator) QuestRowText(tr *i18n.Translator, quest *db.QuestTracking) string {
	var rewardThing string

	switch quest.RewardType {
	case 7:
		// Pokemon reward
		mon := g.GD.GetMonster(quest.Reward, quest.Form)
		if mon != nil {
			rewardThing = tr.T(gamedata.PokemonTranslationKey(quest.Reward))
			if quest.Form != 0 {
				formName := tr.T(gamedata.FormTranslationKey(quest.Form))
				if formName != gamedata.FormTranslationKey(quest.Form) {
					rewardThing += " " + formName
				}
			}
		} else {
			rewardThing = fmt.Sprintf("%s %d %d", tr.T("tracking.unknown_monster"), quest.Reward, quest.Form)
		}

	case 3:
		// Stardust reward
		if quest.Reward > 0 {
			rewardThing = tr.Tf("tracking.reward_stardust_min_fmt", quest.Reward)
		} else {
			rewardThing = tr.T("tracking.reward_stardust")
		}

	case 2:
		// Item reward
		item := g.GD.GetItem(quest.Reward)
		if item != nil {
			rewardThing = tr.T(gamedata.ItemTranslationKey(quest.Reward))
		} else {
			rewardThing = fmt.Sprintf("%s %d", tr.T("tracking.unknown_item"), quest.Reward)
		}

	case 12:
		// Mega energy reward
		if quest.Reward == 0 {
			rewardThing = tr.T("tracking.reward_mega_energy")
		} else {
			mon := g.GD.GetMonster(quest.Reward, 0)
			var monsterName string
			if mon != nil {
				monsterName = tr.T(gamedata.PokemonTranslationKey(quest.Reward))
			} else {
				monsterName = fmt.Sprintf("%s %d", tr.T("tracking.unknown_monster"), quest.Reward)
			}
			rewardThing = tr.Tf("tracking.reward_mega_energy_fmt", monsterName)
		}

	case 4:
		// Candy reward
		if quest.Reward == 0 {
			rewardThing = tr.T("tracking.reward_candy")
		} else {
			mon := g.GD.GetMonster(quest.Reward, 0)
			var monsterName string
			if mon != nil {
				monsterName = tr.T(gamedata.PokemonTranslationKey(quest.Reward))
			} else {
				monsterName = fmt.Sprintf("%s %d", tr.T("tracking.unknown_monster"), quest.Reward)
			}
			rewardThing = tr.Tf("tracking.reward_candy_fmt", monsterName)
		}
	}

	s := tr.Tf("tracking.reward_fmt", rewardThing)

	if quest.Amount > 0 {
		s += " " + tr.Tf("tracking.reward_min_fmt", quest.Amount)
	}

	if quest.Distance != 0 {
		s += " | " + tr.Tf("tracking.distance_fmt", quest.Distance)
	}

	s += " " + standardText(tr, quest.Template, g.DefaultTemplateName, quest.Clean)

	return s
}
