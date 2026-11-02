export default {
  "nativeAccess": {
    "copyBase": "Copy Base URL", "copyKey": "Copy API key", "copyExample": "Copy example",
    "channelHint": "Composite groups bind here once. Choose any supplier below to edit its model prices and mappings. Prices retain their supplier identity; separate channels can use different prices.",
    "title": "Composite native access",
    "hint": "Choose a default client and native endpoint. This key also works at the other endpoints listed for each model. Requests are never converted between protocols.",
    "client": "Client",
    "endpoint": "Native endpoint",
    "baseUrl": "SDK Base URL",
    "requestUrl": "Full request URL",
    "models": "Models supporting this endpoint",
    "noModels": "No model supports this client and endpoint combination.",
    "allModels": "View native endpoints for every model",
    "custom": "Custom client",
    "loadFailed": "Could not load native capabilities. Retry by selecting the group again."
  },
  "longContext": {
    "title": "Long-context tier pricing",
    "hint": "Model override > platform default > model price preset > 0. Blank inherits; 0 disables tiering even if enabled on the account. A positive threshold enables it again. Thresholds apply to the whole request input including cache reads/writes and never change caching or the context window. Retained high-tier prices are reused after re-enabling.",
    "platformDefaults": "Platform default thresholds",
    "modelOverrides": "Model threshold and high-tier overrides",
    "inherit": "Inherit existing preset",
    "threshold": "Input threshold (0 disables)",
    "mode": "High-tier pricing",
    "prices": "Custom prices",
    "multipliers": "Custom multipliers",
    "input": "Input",
    "output": "Output",
    "cache_write": "Cache write (5m)",
    "cache_write_1h": "Cache write (1h)",
    "cache_read": "Cache read",
    "priceUnit": "USD / 1M tokens",
    "factorUnit": "× base price",
    "invalid": "Check thresholds, unique exact model IDs and high-tier prices. Custom pricing requires input and output; blank cache meters inherit."
  }
}
