package commands

import (
	"errors"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/personalshiny"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// AccountCommand manages the target's linked game accounts (player IDs) for
// personal shiny/background tracking. Player IDs and names are read from
// ctx.VerbatimArgs so case and underscores survive.
type AccountCommand struct{}

func (c *AccountCommand) Name() string      { return "cmd.account" }
func (c *AccountCommand) Aliases() []string { return nil }

func (c *AccountCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if ctx.PlayerAccounts == nil {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.account.unavailable")}}
	}
	if help := helpArgReply(ctx, args, "msg.account.usage"); help != nil {
		return []bot.Reply{*help}
	}
	verbatim := bot.StripTargetTokens(ctx.VerbatimArgs)
	if len(verbatim) == 0 {
		// Like bare !location: show the current state, then the usage line.
		return append(c.list(ctx), bot.Reply{Text: inlineUsage(ctx, "msg.account.usage")})
	}

	enTr := ctx.Translations.For("en")
	sub := strings.ToLower(verbatim[0])
	is := func(key string) bool {
		return sub == strings.ToLower(tr.T(key)) || sub == strings.ToLower(enTr.T(key))
	}
	rest := verbatim[1:]
	deny := []bot.Reply{{React: "🙅", Text: tr.T("msg.account.admin_only")}}
	usage := []bot.Reply{{Text: inlineUsage(ctx, "msg.account.usage")}}

	switch {
	case is("arg.list"):
		return c.list(ctx)
	case is("arg.add"):
		if !c.canWrite(ctx) {
			return deny
		}
		// `add <player-id>` (unnamed) or `add <name> <player-id>`. Player IDs
		// are a single token, so anything beyond two args is a usage error.
		var label, playerID string
		switch len(rest) {
		case 1:
			playerID = rest[0]
		case 2:
			label, playerID = rest[0], rest[1]
		default:
			return usage
		}
		return c.result(ctx, ctx.PlayerAccounts.Add(ctx.TargetID, playerID, label), label,
			tr.Tf("msg.account.added", personalshiny.DisplayName(db.PlayerAccount{PlayerID: playerID, Label: label})))
	case is("arg.remove"):
		if !c.canWrite(ctx) {
			return deny
		}
		if len(rest) < 1 {
			return usage
		}
		ref := strings.Join(rest, " ")
		a, err := ctx.PlayerAccounts.Resolve(ctx.TargetID, ref)
		if err != nil {
			log.Errorf("account: resolve %s/%q: %v", ctx.TargetID, ref, err)
			return []bot.Reply{{React: "🙅"}}
		}
		if a == nil {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.account.not_found", ref)}}
		}
		return c.result(ctx, ctx.PlayerAccounts.Remove(ctx.TargetID, a.PlayerID), "",
			tr.Tf("msg.account.removed", personalshiny.DisplayName(*a)))
	case is("arg.rename"):
		if !c.canWrite(ctx) {
			return deny
		}
		if len(rest) < 1 {
			return usage
		}
		a, err := ctx.PlayerAccounts.Resolve(ctx.TargetID, rest[0])
		if err != nil {
			log.Errorf("account: resolve %s/%q: %v", ctx.TargetID, rest[0], err)
			return []bot.Reply{{React: "🙅"}}
		}
		if a == nil {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.account.not_found", rest[0])}}
		}
		label := strings.Join(rest[1:], " ")
		ok := tr.Tf("msg.account.renamed", personalshiny.DisplayName(*a), label)
		if label == "" {
			ok = tr.Tf("msg.account.name_cleared", personalshiny.DisplayName(*a))
		}
		return c.result(ctx, ctx.PlayerAccounts.SetLabel(ctx.TargetID, a.PlayerID, label), label, ok)
	}
	return usage
}

// canWrite: admins; delegated channel admins for the channel the command
// runs in; otherwise only self-service on the caller's own user. Other
// non-user targets (another channel, a webhook) need full admin.
func (c *AccountCommand) canWrite(ctx *bot.CommandContext) bool {
	isUserTarget := strings.HasSuffix(ctx.TargetType, ":user")
	if ctx.IsAdmin {
		return true
	}
	if !isUserTarget && ctx.Permissions.ChannelTracking && ctx.ChannelID != "" && ctx.TargetID == ctx.ChannelID {
		return true
	}
	return isUserTarget && ctx.TargetID == ctx.UserID &&
		ctx.Config != nil && ctx.Config.PlayerAccounts.SelfService
}

func (c *AccountCommand) result(ctx *bot.CommandContext, err error, label, ok string) []bot.Reply {
	tr := ctx.Tr()
	switch {
	case err == nil:
		if ctx.ReloadFunc != nil {
			ctx.ReloadFunc()
		}
		return []bot.Reply{{React: "✅", Text: ok}}
	case errors.Is(err, store.ErrDuplicateAccount):
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.account.duplicate")}}
	case errors.Is(err, store.ErrDuplicateAccountLabel):
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.account.duplicate_name", label)}}
	case errors.Is(err, store.ErrTooManyAccounts):
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.account.too_many", personalshiny.MaxPlayers)}}
	case errors.Is(err, store.ErrInvalidPlayerID):
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.account.invalid_id")}}
	case errors.Is(err, store.ErrInvalidAccountLabel):
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.account.invalid_name")}}
	case errors.Is(err, store.ErrAccountNotFound):
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.account.not_found", "")}}
	}
	log.Errorf("account: %s: %v", ctx.TargetID, err)
	return []bot.Reply{{React: "🙅"}}
}

func (c *AccountCommand) list(ctx *bot.CommandContext) []bot.Reply {
	tr := ctx.Tr()
	list, err := ctx.PlayerAccounts.List(ctx.TargetID)
	if err != nil {
		log.Errorf("account: list %s: %v", ctx.TargetID, err)
		return []bot.Reply{{React: "🙅"}}
	}
	if len(list) == 0 {
		return []bot.Reply{{Text: tr.T("msg.account.none")}}
	}
	var sb strings.Builder
	sb.WriteString(tr.T("msg.account.list_header"))
	for _, a := range list {
		sb.WriteString("\n" + tr.Tf("msg.account.list_row", personalshiny.DisplayName(a), a.CreatedAt.Format("2006-01-02")))
	}
	return bot.SplitTextReply(sb.String())
}
