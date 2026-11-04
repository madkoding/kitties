# Limits

Kitties does not limit tokens by default, so a large document is read whole. What can still stop
a request comes from the model, not from Kitties.

## Removed (Kitties')

| Before | Now |
| --- | --- |
| Text of a file cut at 30,000 characters (60,000 per message) | Whole, up to half of the model's context window; a longer file keeps its start as written and the rest is read in parts and condensed by the model (once, kept next to the file) |
| 6 files and 20 MB per message; 2 MB a text, 10 MB a PDF | 20 files and 100 MB; 25 MB a text, 32 MB a PDF, 50 MB other |
| Answers of 4096 tokens | 16384 (`max_tokens` of the profile) |
| Daily and per-task token budget | `0` (none) by default; `limits.day_tokens` / `limits.max_tokens` cap them if you want |
| A thread cut at a fixed number of turns | Fitted to the model's window (pictures and documents counted); the oldest go first |
| 4 hours per task | No time limit by default; `limits.max_seconds` caps one |
| Rounds capped (8 windows on the desktop, 40 connector rounds per answer) | No cap; `limits.max_rounds` is the size of a checkpoint window; the stuck-screen check still nudges a kitty that repeats itself |

`limits.attended: true` is the opt-in cautious mode: it stops at the end of its rounds and waits for a "continue", with no token or time cap of its own.

## Not removable (the model's)

- **Context window.** `context_tokens` of the profile; by default the usual one of the provider
  (Gemini 1,000,000; Ollama 32,768; Groq, DeepSeek, Qwen, Mistral 128,000; else 200,000). Set your
  model's real window: Kitties fits files and threads to it instead of failing.
- **Answer size.** The most one answer can write; Anthropic requires the value. Raise `max_tokens`
  if the model allows more, lower it if it allows less (it would refuse the request).
- **Pictures (5 MB) and PDFs (32 MB).** The providers' own size limits.
- **Your provider's quota.** Rate limits and the plan's usage are the provider's.
