# AI Command Assistant

PoracleNG includes an optional AI assistant that translates natural language into Poracle tracking commands. Users type `!ask` followed by what they want in plain English, and the AI suggests the correct command(s).

```
User: !ask track shiny pikachu with good great league PVP
Bot:  AI suggests:
      !track pikachu iv0 great100
      React ✅ to run, or ❌ to cancel.
```

## How It Works

The `!ask` command sends the user's text to a local or cloud AI model via the OpenAI-compatible chat completions API. The model uses a system prompt containing the full Poracle command reference to generate correct commands. The bot shows the suggestion and waits for user confirmation before executing.

The AI is **never given access** to user data, tracking rules, or the database. It only translates text → commands.

## Setup

### 1. Choose a Provider

Any service that supports the OpenAI chat completions API (`/v1/chat/completions`) works:

| Provider | Cost | Speed | Setup |
|----------|------|-------|-------|
| **Ollama** (local) | Free | 1-5s | Install on your server |
| **OpenRouter** | Pay-per-token | 1-3s | API key only |
| **Groq** | Free tier available | <1s | API key only |
| **OpenAI** | Pay-per-token | 1-2s | API key only |
| **LM Studio** (local) | Free | 1-5s | Desktop app |

### 2. Configure

Add to `config/config.toml`:

```toml
[ai]
enabled = true
provider_url = "http://localhost:11434/v1"  # Your provider URL
api_key = ""                                 # Empty for local Ollama
model = "qwen2.5:1.5b"                      # Model name
```

### 3. Provider-Specific Setup

#### Ollama (Recommended — Free, Local)

[Ollama](https://ollama.ai) runs models locally on your server. Models are loaded into RAM only when processing a request and unloaded after idle (default 5 minutes), so there's zero RAM cost between requests.

```bash
# Install Ollama
curl -fsSL https://ollama.ai/install.sh | sh

# Pull a model (choose one)
ollama pull qwen2.5:1.5b    # 1GB, fast, good for structured tasks
ollama pull qwen2.5:3b      # 2GB, better quality
ollama pull phi3:mini        # 2.2GB, Microsoft, good reasoning
ollama pull gemma2:2b        # 1.5GB, Google
```

Config:
```toml
[ai]
enabled = true
provider_url = "http://localhost:11434/v1"
api_key = ""
model = "qwen2.5:1.5b"
```

Docker users — add Ollama as a sidecar:
```yaml
services:
  ollama:
    image: ollama/ollama
    volumes:
      - ollama_data:/root/.ollama
    # GPU support (optional):
    # deploy:
    #   resources:
    #     reservations:
    #       devices:
    #         - capabilities: [gpu]

  poracle:
    image: ghcr.io/jfberry/poracleng:main
    environment:
      # Point AI at the Ollama container
      - AI_PROVIDER_URL=http://ollama:11434/v1
    # ... rest of your config

volumes:
  ollama_data:
```

After starting, pull a model inside the container:
```bash
docker exec -it ollama ollama pull qwen2.5:1.5b
```

#### OpenRouter (Many Models, One API Key)

[OpenRouter](https://openrouter.ai) provides access to hundreds of models from different providers through a single API key. Good for experimentation.

1. Sign up at https://openrouter.ai
2. Create an API key in your account settings
3. Browse models at https://openrouter.ai/models

Config:
```toml
[ai]
enabled = true
provider_url = "https://openrouter.ai/api/v1"
api_key = "sk-or-v1-..."
model = "qwen/qwen-2.5-7b-instruct"
# Other options:
# model = "google/gemma-2-2b-it"          # Free
# model = "meta-llama/llama-3.1-8b-instruct"  # Free
# model = "anthropic/claude-3.5-haiku"    # Fast, cheap
```

#### Groq (Fast, Free Tier)

[Groq](https://groq.com) offers very fast inference with a generous free tier.

1. Sign up at https://console.groq.com
2. Create an API key

Config:
```toml
[ai]
enabled = true
provider_url = "https://api.groq.com/openai/v1"
api_key = "gsk_..."
model = "llama-3.1-8b-instant"
```

#### OpenAI

```toml
[ai]
enabled = true
provider_url = "https://api.openai.com/v1"
api_key = "sk-..."
model = "gpt-4o-mini"
```

## Recommended Models

| Use Case | Model | Size | Notes |
|----------|-------|------|-------|
| **Budget local** | `qwen2.5:1.5b` | 1GB | Good enough for simple commands |
| **Quality local** | `qwen2.5:3b` | 2GB | Better at complex multi-filter commands |
| **Best local** | `qwen2.5:7b` | 4.5GB | Excellent, needs more RAM |
| **Free cloud** | Groq `llama-3.1-8b-instant` | — | Very fast, free tier |
| **Cheap cloud** | OpenAI `gpt-4o-mini` | — | Reliable, ~$0.0001 per request |
| **Best cloud** | Any large model via OpenRouter | — | For complex edge cases |

## Testing

Test the AI endpoint directly:

```bash
curl -X POST http://localhost:3030/api/ai/translate \
  -H "Content-Type: application/json" \
  -H "X-Poracle-Secret: your-secret" \
  -d '{"message": "track shiny pikachu with good great league PVP"}'
```

Expected response:
```json
{"status": "ok", "command": "!track pikachu iv0 great100"}
```

Then test via Discord/Telegram:
```
!ask track perfect dragonite
!ask notify me about level 5 raids nearby
!ask I want water team rocket invasions
!ask track 0% attack pokemon for ultra league PVP
```

## Troubleshooting

**"AI assistant is not configured"**
→ Set `[ai] enabled = true` in config.toml and restart.

**Timeout errors**
→ Local models may take a few seconds on first request (cold start). Increase timeout or use a faster model.

**Wrong commands generated**
→ Try a larger model. The system prompt works best with 3B+ parameter models. Smaller models may miss nuances.

**Ollama not responding**
→ Check `ollama serve` is running. Test with `curl http://localhost:11434/v1/models`.

**OpenRouter/Groq auth errors**
→ Verify your API key. Check provider dashboard for quota/rate limits.

## Customising the System Prompt

The system prompt is embedded in `processor/internal/ai/client.go`. It contains the full Poracle command reference. If you add custom commands or aliases, update the prompt to include them.

The prompt is designed to work with small models (1.5B+) by being explicit and example-heavy. Key principles:
- Low temperature (0.1) for deterministic output
- Max 256 tokens (commands are short)
- "Return ONLY the command" instruction prevents explanations
- Many examples cover common phrasings
