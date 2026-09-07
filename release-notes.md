Sub2API Plus v0.2.0+custom.003

## Highlights

Adds canonical GPT-6 Astra support across OpenAI discovery, Codex catalogs,
generated client configuration, request metadata, and billing.

## Changed

- Lists GPT-6 Astra first while retaining GPT-5.6 Sol as the account-test model.
- Mirrors the official Codex Astra instructions, default low reasoning level,
  multi-agent Ultra preset, and client capability metadata.
- Uses official Standard, Flex, Fast, prompt-cache, and whole-request
  long-context pricing for OpenAI Platform API-key traffic.

## Compatibility and migration

Only the canonical `gpt-6-astra` ID is added. Existing model pricing, including
the configured GPT-5.6 Sol rates, is unchanged. At 272,001 total input tokens
and above, all Astra input and cache tokens are billed at 2x and all output
tokens at 1.5x.

## Known issues

Fast/priority processing is unavailable for GPT-6 Astra with EU data residency.

## Upstream baseline

Official release: v0.2.0
Official commit: aa236488351eb71e120fc2b6fb32e36b0374c918
