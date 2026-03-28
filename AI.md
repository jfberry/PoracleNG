# AI Command Assistant

PoracleNG includes an optional AI assistant that translates natural language into Poracle tracking commands. Users type `!ask` followed by what they want in plain English, and the AI suggests the correct command(s).

```
User: !ask track shiny pikachu with good great league PVP
Bot:  AI suggests:
      `!track pikachu iv0 great100`

      Copy and paste to run.
```

## How It Works

The `!ask` command sends the user's text to a local or cloud AI model via the OpenAI-compatible chat completions API. The model uses a system prompt containing the full Poracle command reference to generate correct commands. The bot shows the suggestion for the user to copy and run.

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
model = "gemma3:12b"                         # Model name
```

### 3. Provider-Specific Setup

#### Ollama (Recommended — Free, Local)

[Ollama](https://ollama.ai) runs models locally on your server. Models are loaded into RAM only when processing a request and unloaded after idle (default 5 minutes), so there's zero RAM cost between requests.

```bash
# Install Ollama
curl -fsSL https://ollama.ai/install.sh | sh

# Pull a model (choose one)
ollama pull gemma3:4b      # 3GB, good balance of quality and speed
ollama pull gemma3:12b     # 8GB, best quality for local use
ollama pull llama3.1:8b    # 4.5GB, good alternative
```

Config:
```toml
[ai]
enabled = true
provider_url = "http://localhost:11434/v1"
api_key = ""
model = "gemma3:12b"
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
docker exec -it ollama ollama pull gemma3:12b
```

#### OpenRouter (Many Models, One API Key)

[OpenRouter](https://openrouter.ai) provides access to hundreds of models from different providers through a single API key. Good for experimentation.

1. Sign up at https://openrouter.ai
2. Create an API key in your account settings
3. Add credits (even $1 is enough for thousands of requests)

Config:
```toml
[ai]
enabled = true
provider_url = "https://openrouter.ai/api/v1"
api_key = "sk-or-v1-..."
model = "google/gemma-3-12b-it"
# Other options:
# model = "meta-llama/llama-3.1-8b-instruct"
# model = "mistralai/mistral-small-3.1-24b-instruct"
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

The system prompt is ~8KB and requires a model that reliably follows system instructions. Models below 7B parameters may ignore the system prompt and generate irrelevant output.

| Use Case | Model | Size | Notes |
|----------|-------|------|-------|
| **Best local** | `gemma3:12b` (Ollama) | 8GB | Excellent accuracy, recommended |
| **Good local** | `gemma3:4b` (Ollama) | 3GB | Good balance of speed and quality |
| **Good local** | `llama3.1:8b` (Ollama) | 4.5GB | Reliable alternative |
| **Best cloud** | `google/gemma-3-12b-it` (OpenRouter) | — | Best tested, ~$0.07/M tokens |
| **Good cloud** | `meta-llama/llama-3.1-8b-instruct` (OpenRouter) | — | Slightly cheaper |
| **Fast cloud** | Groq `llama-3.1-8b-instant` | — | Very fast, free tier |
| **Reliable cloud** | OpenAI `gpt-4o-mini` | — | Most reliable, ~$0.15/M tokens |

**Avoid**: `qwen/qwen-2.5-7b-instruct` — intermittently ignores the system prompt and returns SQL or unrelated content.

## Testing

Test the AI endpoint directly:

```bash
curl -X POST http://localhost:4200/api/ai/translate \
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
��� Local models may take a few seconds on first request (cold start). Increase timeout or use a faster model.

**Wrong commands generated (SQL, generic advice, etc.)**
→ Your model is too small or unreliable with system prompts. Switch to `google/gemma-3-12b-it` or `meta-llama/llama-3.1-8b-instruct`. The system prompt requires 7B+ parameter models that reliably follow system instructions.

**Ollama not responding**
→ Check `ollama serve` is running. Test with `curl http://localhost:11434/v1/models`.

**OpenRouter/Groq auth errors**
→ Verify your API key. Check provider dashboard for quota/rate limits.

## Customising the System Prompt

The system prompt is embedded in `processor/internal/ai/prompt.go`. It contains the full Poracle command reference. If you add custom commands or aliases, update the prompt to include them.

The prompt is designed to work with 7B+ models by being explicit and example-heavy. Key principles:
- Low temperature (0.1) for deterministic output
- Max 256 tokens (commands are short)
- User message wrapper reinforces "commands only" instruction
- Post-processing extracts `!` command lines from noisy model output
- Many examples cover common phrasings
