## ADDED Requirements

### Requirement: Security audit must use one canonical content contract

Content Moderation and Prompt Audit SHALL consume the same canonical protocol extraction result. Account type, session affinity, routing, retries, probes, protocol transforms, and upstream compatibility adapters MUST NOT select a different extraction path or bypass the security-audit gate.

#### Scenario: API-key and OAuth requests carry the same payload

- **WHEN** an API-key-owned account and an OAuth-owned account receive the same content-bearing inbound protocol payload
- **THEN** both requests MUST produce the same canonical audit segments before account selection
- **THEN** account selection or credential normalization MUST NOT change whether the content is audited

#### Scenario: A protocol adapter adds a content field

- **WHEN** a supported endpoint begins accepting a new user, tool, search, instruction, or structured content field
- **THEN** the canonical extraction contract and real-payload tests MUST be updated in the same change
- **THEN** the field MUST be available to both audit-engine selection policies without independent protocol parsers

### Requirement: Content Moderation must use direct-user attribution

Content Moderation SHALL select only current direct-user text and images from the canonical result. Prompt Audit SHALL retain complete canonical coverage. A valid canonical document that contains only content excluded by Content Moderation is an ordinary empty moderation selection, not an extraction failure.

#### Scenario: Platform context accompanies a user message

- **WHEN** a request contains a current direct-user message together with instructions, system/developer context, tool definitions, reusable prompt variables, reasoning, or historical content
- **THEN** Content Moderation MUST include only the current direct-user message text and images
- **THEN** Prompt Audit MUST retain the canonical context required by its full or latest-turn policy

#### Scenario: A turn has no direct-user submission

- **WHEN** a valid turn contains only assistant/model content, tool calls/results, approvals, instructions, or prompt variables
- **THEN** Content Moderation MUST skip the external Moderations request
- **THEN** Prompt Audit MUST continue to apply its configured selection policy

### Requirement: Tool and external-source results must remain visible without user attribution

Client-submitted tool results and other external-source content SHALL be classified as current client-controlled canonical input. Prompt Audit SHALL cover that input. Content Moderation MUST NOT attribute tool, assistant/model, instruction, or reusable-prompt content to the direct user. Structured results MUST be converted to deterministic text without logging their raw content.

#### Scenario: Responses submits a function result

- **WHEN** a Responses HTTP or WebSocket turn contains `function_call_output.output`, `custom_tool_call_output.output`, `tool_search_output.output`, or `mcp_tool_call_output.output`
- **THEN** Prompt Audit full and latest-turn snapshots MUST include and prioritize every current tool result rather than an older user message
- **THEN** Content Moderation MUST produce no input for a tool-result-only turn

#### Scenario: Responses submits current official tool items

- **WHEN** a Responses request carries reusable `prompt.variables`, official `tool_search_output.tools`, local or hosted shell output, apply-patch output, computer output, MCP items, code-interpreter output, or program/program-output items
- **THEN** every textual variable, argument, result, error, code, and dynamic tool definition MUST be normalized by the canonical extractor
- **THEN** API-key and OAuth routing MUST receive identical audit coverage

#### Scenario: Structured result mixes ordinary text and media

- **WHEN** a structured tool result contains ordinary text beside image/file URLs, data URLs, long base64, encrypted content, or computer screenshots
- **THEN** ordinary text MUST remain in the canonical result and Prompt Audit
- **THEN** encoded media and opaque payloads MUST NOT enter Prompt Audit text persistence
- **THEN** recognized images MUST retain their canonical source and role attribution
- **THEN** Content Moderation MUST select an image only when it belongs to a current direct-user item

#### Scenario: Other protocols submit tool results

- **WHEN** Chat Completions uses a tool-role message, Anthropic uses a tool_result block, or Gemini uses functionResponse
- **THEN** the current result text or deterministic structured representation MUST be audited by Prompt Audit
- **THEN** Content Moderation MUST skip the tool-result-only turn

### Requirement: Inbound roles and prompt context must not create bypasses

Inbound role labels SHALL be treated as untrusted request data for Prompt Audit. Current instructions, tool definitions, and current message content MUST remain canonical and auditable when latest-turn narrowing is enabled. Content Moderation SHALL separately enforce direct-user attribution.

#### Scenario: Client submits a current assistant or model message

- **WHEN** the last Chat, Anthropic, Responses, or Gemini content item claims an assistant or model role
- **THEN** Prompt Audit MUST treat that current item as client-controlled and prioritize it instead of falling back to an older user message
- **THEN** Content Moderation MUST NOT treat it as a direct-user policy input

#### Scenario: Request supplies instructions and tool definitions

- **WHEN** an accepted payload contains instructions, system context, or tool/function definitions
- **THEN** Prompt Audit MUST audit that context through canonical segments
- **THEN** Content Moderation MUST exclude that context from the direct-user policy input

### Requirement: Supported specialized endpoints must extract their canonical text

The shared extractor SHALL cover specialized endpoint payloads instead of relying on generic prompt-key fallbacks.

#### Scenario: Alpha Search carries queries

- **WHEN** Alpha Search contains one or more non-empty `commands.search_query[].q` values or Responses-shaped recent conversation in `input`
- **THEN** every current query and direct-user input delta MUST be included in both audit engines before routing and billing

#### Scenario: Live or Embeddings carries content

- **WHEN** Live carries initial session instructions/input, a Sideband session/item/response update, or Embeddings carries a string/string-array input
- **THEN** all current text values MUST remain available to Prompt Audit
- **THEN** Content Moderation MUST include direct-user Live input and embedding strings but exclude Live session instructions, assistant/model items, and tool items
- **THEN** every Live Sideband client frame MUST be audited before its upstream write

#### Scenario: Live carries transcription context

- **WHEN** an initial Live HTTP session or session update carries legacy `input_audio_transcription.prompt`/`keywords` or current `audio.input.transcription.prompt`/`keywords`
- **THEN** the initial request and Sideband update MUST use the `openai_live` adapter
- **THEN** every prompt and keyword MUST be included in Prompt Audit before billing, concurrency acquisition, or an upstream write
- **THEN** Content Moderation MUST exclude that transcription configuration from direct-user policy input

#### Scenario: Responses WebSocket uses a nested envelope

- **WHEN** a `response.create` frame carries instructions or input under `response`
- **THEN** it MUST produce the same canonical segments as the equivalent top-level frame
- **THEN** non-content WebSocket control frames MAY remain explicit no-content cases

#### Scenario: Responses passthrough receives a non-create data frame

- **WHEN** a passthrough connection receives `conversation.item.create`, `session.update`, another text frame, or a binary client frame
- **THEN** the frame MUST enter the same security-audit hook before any upstream write
- **THEN** an invalid or rejected frame MUST produce zero upstream writes

### Requirement: Content-bearing extraction failures must be visible and fail closed in blocking mode

Every enabled audit engine SHALL count extraction attempts, successes, empty selections, and failures. A canonical payload classified as content-bearing but producing neither an auditable text segment nor a recognized image, or containing any non-empty content item that cannot be completely normalized, SHALL be an extraction failure, not an ordinary empty or successful request. Extractable sibling items MUST NOT hide a partial extraction failure. After successful canonical extraction, an engine MAY select no content when its documented attribution policy excludes every canonical item.

#### Scenario: Observe mode cannot extract expected content

- **WHEN** a content-bearing payload cannot be normalized while an audit engine is asynchronous or observe-only
- **THEN** the request MAY continue
- **THEN** the engine MUST increment extraction failure metrics and emit a structured log without raw content

#### Scenario: Blocking mode cannot extract expected content

- **WHEN** a content-bearing payload cannot be normalized while either blocking audit engine applies
- **THEN** the request MUST be rejected before account selection, billing, concurrency acquisition, or upstream writes
- **THEN** the response MUST use the engine's existing unavailable or invalid audit error envelope
- **THEN** Content Moderation extraction failures MUST use `content_moderation_unavailable`, not the policy-violation code

#### Scenario: A payload mixes supported and unknown content items

- **WHEN** one item produces auditable text but a non-empty sibling content item has no canonical adapter
- **THEN** the payload MUST be classified as an incomplete extraction
- **THEN** a blocking audit mode MUST fail closed instead of treating the extracted sibling as complete coverage

#### Scenario: A known type contains an unknown sibling field

- **WHEN** a known message, tool item, or control event contains valid extracted content and an additional unknown non-empty field
- **THEN** the payload MUST be classified as incomplete
- **THEN** the successful known field MUST NOT hide the unsupported sibling

#### Scenario: A request or session object contains an unknown sibling field

- **WHEN** a Responses request root, nested `response`, Responses/Live session, or Live transcription object contains valid extracted text and an additional unknown non-empty field
- **THEN** the payload MUST be classified as incomplete
- **THEN** blocking mode MUST reject it before any downstream side effect

#### Scenario: Structured serialization fails

- **WHEN** a recognized structured value cannot be sanitized or deterministically serialized
- **THEN** extraction MUST set `Incomplete` instead of silently dropping the value
- **THEN** blocking mode MUST fail closed

#### Scenario: Explicit control frame contains no content

- **WHEN** a supported WebSocket control frame is explicitly classified as non-content-bearing
- **THEN** it MAY continue without an audit task
- **THEN** it MUST be counted separately from extraction failures

#### Scenario: Recognized media-only input contains no text

- **WHEN** a supported message contains only recognized image blocks
- **THEN** Prompt Audit MAY classify it as explicit no-text
- **THEN** Content Moderation MUST preserve the image input for image moderation

#### Scenario: Embeddings input uses unsupported token IDs

- **WHEN** Embeddings input contains token IDs that the canonical layer cannot reliably decode
- **THEN** the request MUST NOT count numeric JSON as successfully audited text
- **THEN** a blocking audit mode MUST fail closed

### Requirement: Coverage tests must prove extraction semantics and side-effect order

Every supported content-bearing endpoint and payload family SHALL have tests using production-shaped JSON. Route-order tests alone SHALL NOT satisfy content coverage.

#### Scenario: Protocol coverage test executes

- **WHEN** the security-audit test suite runs
- **THEN** it MUST pass real payloads through the canonical extractor, Content Moderation, and Prompt Audit
- **THEN** it MUST assert Prompt Audit coverage and correct tool-result priority
- **THEN** it MUST assert that Content Moderation includes direct-user input while excluding instructions, assistant/model content, reusable prompt variables, and tool content

#### Scenario: Gateway ordering test executes

- **WHEN** HTTP and WebSocket security-audit ordering tests run for API-key and OAuth paths
- **THEN** blocking or invalid extraction decisions MUST prove zero account-selection, billing, concurrency, and upstream side effects

#### Scenario: Content Moderation extraction is unavailable

- **WHEN** Content Moderation pre-block cannot completely extract a content-bearing request
- **THEN** the coordinator MUST classify the gateway outcome as unavailable with HTTP 503 and `content_moderation_unavailable`
- **THEN** logs and metrics MUST NOT classify that system failure as a confirmed policy block
