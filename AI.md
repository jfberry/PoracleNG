# !ask Command — Natural Language Tracking

PoracleNG includes an `!ask` command that translates natural language into Poracle tracking commands. Users type `!ask` followed by what they want in plain English, and the bot suggests the correct command(s).

```
User: !ask perfect dragonite nearby
Bot:  Suggested command(s):
      `!track dragonite iv100 d1000`

      Copy and paste to run.
```

## How It Works

The `!ask` command uses a built-in NLP parser — no external AI service or API key needed. The parser:

1. Normalizes input (lowercase, strips filler like "notify me about", "I want")
2. Detects command intent (raid, quest, invasion, etc.)
3. Matches tokens against vocabularies built from game data (pokemon names, items, types, forms, moves)
4. Applies synonym expansion (hundo→iv100, nearby→d1000, pvp→great5)
5. Assembles the Poracle command(s)

Vocabularies are built at startup from the same game data the processor uses for matching. No external calls, zero latency, zero cost.

## Setup

Add to `config/config.toml`:

```toml
[ai]
enabled = true
```

That's it. Restart the processor and alerter.

## What It Understands

**Pokemon tracking:**
- "perfect dragonite" → `!track dragonite iv100`
- "nundo pokemon within 1km" → `!track everything iv0 maxiv0 d1000`
- "alolan vulpix" → `!track vulpix form:alolan`
- "shadow pokemon with good IVs" → `!track everything form:shadow iv80`
- "great league rank 1 azumarill" → `!track azumarill great1`
- "XXL pokemon" → `!track everything size:xxl`
- "hundos for pikachu eevee and dragonite" → 3 separate `!track` commands

**Raids:** "mega raids", "level 5 raids nearby", "mewtwo raids with psystrike"

**Quests:** "stardust quests", "golden razz berry quests", "rare candy quests"

**Invasions:** "team rocket water female", "kecleon pokestop"

**Other:** lures, nests, gyms, forts, max battles

**Removal:** "stop tracking dragonite" → `!untrack dragonite`

**Pokemon names are fuzzy-matched:** "mr mime", "mrmime", and "mr. mime" all resolve correctly. Same for "farfetchd"→"farfetch'd", "hooh"→"ho-oh", etc.

**Existing Poracle syntax passes through:** if you type "iv95-99" or "d2000" or "great5", the parser recognizes and preserves them.

## AI Fallback (Optional)

If the NLP parser can't understand an input, it can optionally fall back to an AI model. This requires an OpenAI-compatible API:

```toml
[ai]
enabled = true
fallback_to_ai = true
provider_url = "https://openrouter.ai/api/v1"   # or Ollama, Groq, OpenAI
api_key = "sk-or-v1-..."
model = "google/gemma-3-12b-it"
```

The NLP parser handles 90%+ of requests. The AI fallback catches edge cases like creative phrasing the parser doesn't recognize.

### Provider Options

| Provider | Setup |
|----------|-------|
| **Ollama** (local, free) | `provider_url = "http://localhost:11434/v1"`, no api_key needed |
| **OpenRouter** | `provider_url = "https://openrouter.ai/api/v1"`, requires api_key + credits |
| **Groq** (free tier) | `provider_url = "https://api.groq.com/openai/v1"`, requires api_key |
| **OpenAI** | `provider_url = "https://api.openai.com/v1"`, requires api_key |

Recommended model: `google/gemma-3-12b-it` (tested, reliable with the Poracle system prompt).

## Testing

```bash
# Test the NLP parser directly
curl -X POST http://localhost:4200/api/ai/translate \
  -H "Content-Type: application/json" \
  -H "X-Poracle-Secret: your-secret" \
  -d '{"message": "track perfect dragonite nearby"}'

# Expected: {"status":"ok","command":"!track dragonite iv100 d1000"}
```

Then test via Discord/Telegram:
```
!ask track perfect dragonite
!ask level 5 raids nearby
!ask stardust quests
!ask team rocket water invasions
!ask stop tracking bulbasaur
```

## Troubleshooting

**"!ask is not configured"** → Set `[ai] enabled = true` in config.toml and restart.

**Wrong command generated** → The NLP parser uses keyword matching, so very creative phrasings may not work. Try rephrasing, or enable `fallback_to_ai = true` with an AI provider.

**Pokemon name not recognized** → The parser uses game data translations. If a pokemon was recently added, restart the processor to re-download game data.
