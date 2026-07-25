# Model Pricing Data

This directory contains the Sub2API Plus model-pricing mirror and its local fallback copy.

## Source
The default runtime source is this repository's `main` branch:
- Data: https://raw.githubusercontent.com/luckykuang/sub2api-plus/main/backend/resources/model-pricing/model_prices_and_context_window.json
- SHA-256: https://raw.githubusercontent.com/luckykuang/sub2api-plus/main/backend/resources/model-pricing/model_prices_and_context_window.sha256
- Upstream data source: https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json

## Purpose
This local copy serves as a fallback when the remote file cannot be downloaded due to:
- Network restrictions
- Firewall rules
- DNS resolution issues
- GitHub being blocked in certain regions
- Docker container network limitations

## Update Process
The pricingService will:
1. First attempt to download the latest version from GitHub
2. If download fails, use this local copy as fallback
3. Log a warning when using the fallback file

## Manual Update
To manually update this file with the latest pricing data (if automation is unavailable):
```bash
curl -fsS https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json -o model_prices_and_context_window.json
shasum -a 256 model_prices_and_context_window.json | awk '{print $1}' > model_prices_and_context_window.sha256
```

## File Format
The file contains JSON data with model pricing information including:
- Model names and identifiers
- Input/output token costs
- Context window sizes
- Model capabilities

Last updated: 2025-08-10
