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

Content Moderation SHALL select only current direct-user text and images from the canonical result. Prompt Audit SHALL scan conversation text from that same result and MUST NOT treat tool/function definitions or structured tool-call arguments as prompt text. A valid canonical document that contains only content excluded by Content Moderation is an ordinary empty moderation selection, not an extraction failure.

#### Scenario: Platform context accompanies a user message

- **WHEN** a request contains a current direct-user message together with instructions, system/developer context, tool definitions, reusable prompt variables, reasoning, or historical content
- **THEN** Content Moderation MUST include only the current direct-user message text and images
- **THEN** Prompt Audit MUST retain conversation text required by its full or latest-turn policy and MUST omit tool/function definitions

#### Scenario: A turn has no direct-user submission

- **WHEN** a valid turn contains only assistant/model content, tool calls/results, approvals, instructions, or prompt variables
- **THEN** Content Moderation MUST skip the external Moderations request
- **THEN** Prompt Audit MUST continue to apply its configured selection policy

### Requirement: Tool and external-source results must remain visible without user attribution

Client-submitted tool results and other external-source content SHALL be classified as current client-controlled canonical input. The canonical extractor SHALL retain that input. Prompt Audit MUST NOT scan structured tool-call arguments, function outputs, tool-role outputs, or static tool schemas as prompt text. Content Moderation MUST NOT attribute tool, assistant/model, instruction, or reusable-prompt content to the direct user. Structured results MUST be converted to deterministic text without logging their raw content.

#### Scenario: Responses submits a function result

- **WHEN** a Responses HTTP or WebSocket turn contains `function_call_output.output`, `custom_tool_call_output.output`, `tool_search_output.output`, or `mcp_tool_call_output.output`
- **THEN** Prompt Audit full and latest-turn snapshots MUST omit structured function/tool outputs from scan text
- **THEN** Content Moderation MUST produce no input for a tool-result-only turn

#### Scenario: Responses submits current official tool items

- **WHEN** a Responses request carries reusable `prompt.variables`, official `tool_search_output.tools`, local or hosted shell output, apply-patch output, computer output, MCP items, code-interpreter output, or program/program-output items
- **THEN** every textual variable, argument, result, error, code, and dynamic tool definition MUST be normalized by the canonical extractor
- **THEN** API-key and OAuth routing MUST receive identical audit coverage

#### Scenario: Structured result mixes ordinary text and media

- **WHEN** a structured tool result contains ordinary text beside image/file URLs, data URLs, long base64, encrypted content, or computer screenshots
- **THEN** ordinary text MUST remain in the canonical result; Prompt Audit MUST scan it only when the source is conversation text, not a tool schema or structured tool call
- **THEN** encoded media and opaque payloads MUST NOT enter Prompt Audit text persistence
- **THEN** recognized images MUST retain their canonical source and role attribution
- **THEN** Content Moderation MUST select an image only when it belongs to a current direct-user item

#### Scenario: Other protocols submit tool results

- **WHEN** Chat Completions uses a tool-role message, Anthropic uses a tool_result block, or Gemini uses functionResponse
- **THEN** Chat/Anthropic tool-role outputs and structured Gemini/Responses tool-call items MUST NOT become prompt-audit scan text
- **THEN** Content Moderation MUST skip the tool-result-only turn

### Requirement: Inbound roles and prompt context must not create bypasses

Inbound role labels SHALL be treated as untrusted request data for Prompt Audit. Conversation message text remains auditable. Latest-turn narrowing MUST restore the `v0.1.177+custom.003` policy: latest user text plus the nearest preceding assistant/model output, without appending tool definitions. Content Moderation SHALL separately enforce direct-user attribution.

#### Scenario: Client submits a current assistant or model message

- **WHEN** the last Chat, Anthropic, Responses, or Gemini content item claims an assistant or model role
- **THEN** Prompt Audit latest-turn MUST keep the latest user text as the priority segment and MAY include that assistant/model item only as the nearest previous output
- **THEN** Content Moderation MUST NOT treat it as a direct-user policy input

#### Scenario: Request supplies instructions and tool definitions

- **WHEN** an accepted payload contains instructions, system context, or tool/function definitions
- **THEN** Prompt Audit MUST audit instructions and system context as conversation text and MUST omit static tool/function definitions
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
- **THEN** an extraction failure MUST continue to the upstream write unless independent transport validation or a confirmed policy decision rejects the frame

### Requirement: Content extraction failures must be visible and pass through

Every enabled audit engine SHALL count extraction attempts, successes, empty selections, and failures. Unknown item types, unknown Responses/Live frames, unknown sibling fields, valid-JSON unrecognized structures, and other incomplete or unextractable content MUST pass through without an audit-derived block. Successfully extracted sibling content MUST remain available to both engines. A detected extraction failure MUST be recorded independently from an ordinary empty selection, but MUST NOT become a policy violation, unavailable decision, HTTP 503 response, or WebSocket close.

#### Scenario: Observe mode cannot extract expected content

- **WHEN** a content-bearing payload cannot be normalized while an audit engine is asynchronous or observe-only
- **THEN** the request MUST continue
- **THEN** the engine MUST increment extraction failure metrics and emit a safe structured log

#### Scenario: Blocking mode cannot extract expected content

- **WHEN** a content-bearing payload cannot be normalized while either blocking audit engine applies
- **THEN** successfully extracted content MUST still be evaluated by that engine
- **THEN** an empty selection MUST pass through without an audit-derived block
- **THEN** the extraction failure MUST NOT use an unavailable, invalid-response, or policy-violation envelope

#### Scenario: A payload mixes supported and unknown content items

- **WHEN** one item produces auditable text but a non-empty sibling content item has no canonical adapter
- **THEN** the unsupported item MAY be classified as an incomplete extraction for observability
- **THEN** both engines MUST continue to evaluate the successfully extracted sibling content
- **THEN** the unsupported item MUST NOT cause an audit-derived block

#### Scenario: An unknown frame also carries recognized content

- **WHEN** an unsupported Responses or Live envelope type carries a recognized `input`, `instructions`, or nested `response.input` sibling
- **THEN** both engines MUST count and safely log an extraction failure for the unsupported frame
- **THEN** both engines MUST continue to evaluate the successfully extracted sibling content
- **THEN** the extraction failure alone MUST NOT cause an HTTP error or WebSocket close

#### Scenario: A known type contains an unknown sibling field

- **WHEN** a known message, tool item, or control event contains valid extracted content and an additional unknown non-empty field such as `foo` or `future_payload`
- **THEN** extraction MUST succeed
- **THEN** Content Moderation and Prompt Audit MUST continue to scan the extracted text

#### Scenario: A known type contains ignored protocol metadata

- **WHEN** a known request, message, or content block contains extracted user text together with `originator` or `cache_control`
- **THEN** extraction MUST succeed
- **THEN** Content Moderation and Prompt Audit MUST continue to scan the extracted user text

#### Scenario: A request or session object contains an unknown sibling field

- **WHEN** a Responses request root, nested `response`, Responses/Live session, or Live transcription object contains valid extracted text and an additional unknown non-empty field
- **THEN** extraction MUST succeed
- **THEN** Content Moderation and Prompt Audit MUST continue to scan the extracted text

#### Scenario: A non-empty request or session object is wholly unrecognized

- **WHEN** a Responses or Live root, nested `response`, or session object is non-empty but contains no recognized request, control, content, or metadata field
- **THEN** both engines MUST count and safely log an extraction failure
- **THEN** the request MUST pass through without an audit-derived block

#### Scenario: Structured serialization fails

- **WHEN** a recognized structured value cannot be sanitized or deterministically serialized
- **THEN** extraction MUST record a bounded incomplete reason without raw content
- **THEN** successfully extracted sibling content MUST remain auditable
- **THEN** the request MUST pass through if no other content or policy decision blocks it

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
- **THEN** the extraction failure MUST be logged and the request MUST pass through

#### Scenario: An audit exception is observed

- **WHEN** content extraction, audit evaluation, or an audit dependency returns an exception
- **THEN** a structured log MUST include request ID, endpoint, protocol, stage, a stable error code or reason, and available byte counts
- **THEN** the log MUST NOT include raw content, credentials, or unsanitized user fields
- **THEN** extraction exceptions MUST preserve pass-through semantics, while independent dependency failures retain their documented availability decisions
- **THEN** asynchronous persistence, hash-cache, account-side-effect, notification, worker, runtime, and post-upstream audit failures MUST NOT log raw errors, panic values, or recipient addresses

### Requirement: Coverage tests must prove extraction semantics and side-effect order

Every supported content-bearing endpoint and payload family SHALL have tests using production-shaped JSON. Route-order tests alone SHALL NOT satisfy content coverage.

#### Scenario: Protocol coverage test executes

- **WHEN** the security-audit test suite runs
- **THEN** it MUST pass real payloads through the canonical extractor, Content Moderation, and Prompt Audit
- **THEN** it MUST assert Prompt Audit coverage and correct tool-result priority
- **THEN** it MUST assert that Content Moderation includes direct-user input while excluding instructions, assistant/model content, reusable prompt variables, and tool content

#### Scenario: Gateway ordering test executes

- **WHEN** HTTP and WebSocket security-audit ordering tests run for API-key and OAuth paths
- **THEN** extraction failures MUST prove pass-through to the expected downstream side effects
- **THEN** confirmed policy blocks MUST still prove zero account-selection, billing, concurrency, and upstream side effects

#### Scenario: Content Moderation extraction fails

- **WHEN** Content Moderation pre-block cannot completely extract a content-bearing request
- **THEN** the coordinator MUST allow the request to continue without HTTP 503 or `content_moderation_unavailable`
- **THEN** logs and metrics MUST record the extraction failure without classifying it as a confirmed policy block
