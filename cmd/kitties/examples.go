package main

// exampleConfig is what `kitties init` writes: the same file as configs/kitties.example.yaml,
// kept here so the binary is self-contained.
const exampleConfig = `# kitties configuration. Keys come from the usual variables (OPENAI_API_KEY,
# ANTHROPIC_API_KEY, GEMINI_API_KEY, GROQ_API_KEY, OLLAMA_API_KEY...) unless api_key says otherwise.

providers:
  strong:
    kind: anthropic              # openai | codex | anthropic | gemini | ollama | ollama-cloud | groq
    model: claude-sonnet-5-5     # | openrouter | deepseek | qwen | mistral | copilot | custom
  fast:
    kind: openai
    model: gpt-5.5
  local:
    kind: ollama
    model: qwen3:8b
default_provider: strong

kitties:
  - name: mimi
    provider: strong
    soul:
      persona: a patient researcher who loves to explain
      tone: warm and precise
      traits: [curious, thorough]
  - name: tom
    provider: fast
    soul_file: souls/tom.yaml
    replicas: 2                  # tom-1, tom-2

containers:
  runtime: auto                  # auto | docker | podman
  image: ghcr.io/madkoding/kitty-desktop:latest
  memory_mb: 2048
  cpus: 2

gateway:
  listen: 127.0.0.1:7488
  token: ""                      # empty prints a fresh one on every start

connectors: {}
`

// exampleSoul is the sample soul `kitties init` writes next to the configuration.
const exampleSoul = `name: Tom
persona: a hands-on engineer who likes to try things on the desktop before answering
tone: friendly and direct
traits: [practical, curious, honest about what did not work]
skills: [browsing, testing web apps, small scripts]
instructions: |
  When a task needs the browser, open it on your display and describe what you see.
  Say clearly when you could not finish something.
`
