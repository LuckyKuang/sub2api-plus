package securityaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/LuckyKuang/sub2api-plus/internal/auditcontent"
)

var (
	ErrNoPromptText = errors.New("prompt audit request contains no user text")
)

type promptExtractionDiagnostic struct {
	Failed    bool
	ErrorCode string
	Reasons   []auditcontent.IncompleteReason
}

var (
	bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+\-/]+=*`)
	apiKeyPattern = regexp.MustCompile(`(?i)\b(sk|rk|pk|api[_-]?key|token|secret|password)[-_:=\s]+[A-Za-z0-9._~+\-/]{8,}`)
	canaryPattern = regexp.MustCompile(`(?i)([A-Z]+_CANARY_)[A-Za-z0-9_-]+`)
	emailPattern  = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	phonePattern  = regexp.MustCompile(`(?:\+?\d[\d\s().-]{8,}\d)`)
)

var promptAuditClientWrapperTags = []string{
	"environment_context",
	"permission_profile",
	"system-reminder",
	"filesystem",
}

const promptAuditPrioritySeparator = "\x00SUB2API_PROMPT_AUDIT_PRIORITY_END\x00"

type promptSegment struct {
	text string
	user bool
	role string
}

func ExtractPromptSnapshot(req Request) (PromptSnapshot, error) {
	snapshot, _, err := extractPromptSnapshotWithDiagnostics(req, false)
	return snapshot, err
}

// ExtractBlockingPromptSnapshot builds the synchronous guard input.
// When latestTurnOnly is true, the scan window is the latest user turn plus the
// nearest preceding assistant/model turn. When it is false, blocking uses the
// same client-controlled transcript as async review. A request without user
// content cannot be narrowed safely and falls back to that full transcript.
func ExtractBlockingPromptSnapshot(req Request, latestTurnOnly bool) (PromptSnapshot, error) {
	snapshot, _, err := extractPromptSnapshotWithDiagnostics(req, latestTurnOnly)
	return snapshot, err
}

func extractPromptSnapshotWithDiagnostics(req Request, latestTurnOnly bool) (PromptSnapshot, promptExtractionDiagnostic, error) {
	document, err := auditcontent.Extract(req.Protocol, req.Body)
	if err != nil {
		return PromptSnapshot{}, promptExtractionDiagnostic{Failed: true, ErrorCode: "invalid_json"}, errors.New("prompt audit request JSON is invalid")
	}
	diagnostic := promptExtractionDiagnostic{}
	if document.Incomplete {
		diagnostic = promptExtractionDiagnostic{
			Failed: true, ErrorCode: "incomplete_content",
			Reasons: auditcontent.SanitizeIncompleteReasons(document.IncompleteReasons),
		}
	}
	extracted := promptSegmentsFromAuditContent(document, req.Protocol)
	segments := normalizeSegmentsLatestUserFirst(extracted)
	if latestTurnOnly {
		segments = blockingSegmentsLatestUserAndPreviousOutput(extracted)
	}
	if len(segments) == 0 {
		return PromptSnapshot{}, diagnostic, ErrNoPromptText
	}
	scanText, metadataText := buildPrioritizedScanText(segments)
	digest := sha256.Sum256([]byte(metadataText))
	fullPrompt := BuildFullPrompt(metadataText, DefaultFullPromptMaxRunes)
	stage := strings.TrimSpace(req.Stage)
	if stage == "" {
		stage = "http"
	}
	return PromptSnapshot{
		RequestID: req.RequestID, ClientIP: normalizePromptClientIP(req.ClientIP), UserID: req.UserID, UsernameSnapshot: req.Username,
		UserEmailSnapshot: req.UserEmail, APIKeyID: req.APIKeyID, APIKeyNameSnapshot: req.APIKeyName,
		GroupID: cloneInt64Ptr(req.GroupID), GroupName: req.GroupName, Provider: req.Provider,
		Endpoint: req.Endpoint, Protocol: req.Protocol, Model: req.Model,
		PromptHash: hex.EncodeToString(digest[:]), RedactedPreview: BuildPromptPreview(metadataText, DefaultPromptPreviewMaxRunes),
		FullPrompt: fullPrompt, FullPromptTruncated: utf8.RuneCountInString(fullPrompt) < utf8.RuneCountInString(metadataText),
		PromptLength: utf8.RuneCountInString(metadataText), MessageCount: len(segments), Stage: stage,
		ScanText: scanText, BodyBytes: len(req.Body),
	}, diagnostic, nil
}

func promptSegmentsFromAuditContent(document auditcontent.Document, protocol string) []promptSegment {
	allowRolelessMessage := promptAuditAllowsRolelessMessage(protocol)
	segments := make([]promptSegment, 0, len(document.Segments))
	for _, segment := range document.Segments {
		if !isPromptAuditClientControlledSegment(segment, allowRolelessMessage) {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(segment.Role))
		user := role == "user"
		if role == "" && ((segment.Source == auditcontent.SourceMessage && allowRolelessMessage) ||
			segment.Source == auditcontent.SourceSearchQuery ||
			segment.Source == auditcontent.SourceEmbeddingInput ||
			segment.Source == auditcontent.SourceMediaPrompt) {
			user = true
			role = "user"
		}
		if role == "" {
			switch segment.Source {
			case auditcontent.SourceInstruction, auditcontent.SourcePromptVariable:
				role = "system"
			case auditcontent.SourceToolCall, auditcontent.SourceToolDefinition, auditcontent.SourceToolOutput:
				role = "tool"
			case auditcontent.SourceReasoning:
				role = "assistant"
			}
		}
		segText := segment.Text
		if user {
			segText = stripPromptAuditClientWrapperBlocks(segText)
			if segText == "" {
				continue
			}
		}
		segments = append(segments, promptSegment{
			text: segText,
			user: user,
			role: role,
		})
	}
	return segments
}

func isPromptAuditClientControlledSegment(segment auditcontent.Segment, allowRolelessMessage bool) bool {
	switch segment.Source {
	case auditcontent.SourceSearchQuery, auditcontent.SourceEmbeddingInput, auditcontent.SourceMediaPrompt,
		auditcontent.SourceInstruction, auditcontent.SourcePromptVariable,
		auditcontent.SourceToolCall, auditcontent.SourceToolDefinition, auditcontent.SourceToolOutput,
		auditcontent.SourceReasoning:
		return true
	case auditcontent.SourceMessage:
		role := strings.ToLower(strings.TrimSpace(segment.Role))
		switch role {
		case "user", "system", "developer", "assistant", "tool", "model":
			return true
		case "":
			return allowRolelessMessage
		default:
			return false
		}
	default:
		return false
	}
}

func promptAuditAllowsRolelessMessage(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "openai_responses", "openai_live", "gemini":
		return true
	default:
		return false
	}
}

// DefaultPromptPreviewMaxRunes caps how much sanitized prompt text may be
// considered before BuildPromptPreview withholds the majority for storage/UI.
const DefaultPromptPreviewMaxRunes = 96

func normalizeSegmentsLatestUserFirst(values []promptSegment) []string {
	normalized := normalizedPromptSegments(values)
	if len(normalized) == 0 {
		return nil
	}
	result := make([]string, 0, len(normalized))
	for index := len(normalized) - 1; index >= 0; index-- {
		result = append(result, normalized[index].text)
	}
	return result
}

// blockingSegmentsLatestUserAndPreviousOutput limits synchronous guard input
// to the current user turn and the nearest preceding assistant/model turn.
// A request without user content cannot be narrowed safely and falls back to
// the complete client-controlled transcript.
func blockingSegmentsLatestUserAndPreviousOutput(values []promptSegment) []string {
	normalized := normalizedPromptSegments(values)
	latestUserStart := latestUserSegmentStart(normalized)
	if latestUserStart < 0 {
		return normalizeSegmentsLatestUserFirst(values)
	}
	latestUserEnd := latestUserStart
	for latestUserEnd < len(normalized) && isUserSegment(normalized[latestUserEnd]) {
		latestUserEnd++
	}
	currentUserText := make([]string, 0, latestUserEnd-latestUserStart)
	for _, segment := range normalized[latestUserStart:latestUserEnd] {
		currentUserText = append(currentUserText, segment.text)
	}
	selected := []promptSegment{{text: strings.Join(currentUserText, "\n\n"), user: true, role: "user"}}
	for index := latestUserStart - 1; index >= 0; index-- {
		if !isAssistantOutputSegment(normalized[index]) {
			continue
		}
		start := index
		for start > 0 && isAssistantOutputSegment(normalized[start-1]) {
			start--
		}
		selected = append(selected, normalized[start:index+1]...)
		break
	}
	return promptSegmentTexts(selected)
}

func normalizedPromptSegments(values []promptSegment) []promptSegment {
	normalized := make([]promptSegment, 0, len(values))
	for _, value := range values {
		value.text = strings.TrimSpace(value.text)
		if value.text != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}

func latestUserSegmentStart(values []promptSegment) int {
	latest := -1
	for index := len(values) - 1; index >= 0; index-- {
		if isUserSegment(values[index]) {
			latest = index
			break
		}
	}
	for latest > 0 && isUserSegment(values[latest-1]) {
		latest--
	}
	return latest
}

func isUserSegment(segment promptSegment) bool {
	return segment.user || segment.role == "user"
}

func isAssistantOutputSegment(segment promptSegment) bool {
	return segment.role == "assistant" || segment.role == "model"
}

func promptSegmentTexts(values []promptSegment) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.text)
	}
	return result
}

// stripPromptAuditClientWrapperBlocks removes client harness XML from user
// text while keeping the surrounding user-authored sentences. Whole blocks
// are dropped; leftover wrapper-only segments become empty and are omitted.
func stripPromptAuditClientWrapperBlocks(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || !strings.Contains(text, "<") {
		return text
	}
	stripped := text
	for {
		next := stripOnePromptAuditClientWrapperBlock(stripped)
		if next == stripped {
			break
		}
		stripped = next
	}
	stripped = strings.ReplaceAll(stripped, "\r\n", "\n")
	for strings.Contains(stripped, "\n\n\n") {
		stripped = strings.ReplaceAll(stripped, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(stripped)
}

func stripOnePromptAuditClientWrapperBlock(text string) string {
	lower := strings.ToLower(text)
	bestStart, bestEnd := -1, -1
	for _, name := range promptAuditClientWrapperTags {
		openToken := "<" + name
		searchFrom := 0
		for {
			openRel := strings.Index(lower[searchFrom:], openToken)
			if openRel < 0 {
				break
			}
			openAt := searchFrom + openRel
			afterName := openAt + len(openToken)
			if afterName < len(lower) {
				next := lower[afterName]
				if next != '>' && next != '/' && next != ' ' && next != '\t' && next != '\n' && next != '\r' {
					searchFrom = afterName
					continue
				}
			}
			gt := strings.Index(text[openAt:], ">")
			if gt < 0 {
				break
			}
			tagEnd := openAt + gt + 1
			rawTag := text[openAt:tagEnd]
			if strings.HasSuffix(strings.TrimSpace(rawTag), "/>") {
				if bestStart < 0 || openAt < bestStart {
					bestStart, bestEnd = openAt, tagEnd
				}
				break
			}
			closeToken := "</" + name
			closeRel := strings.Index(lower[tagEnd:], closeToken)
			if closeRel < 0 {
				if bestStart < 0 || openAt < bestStart {
					bestStart, bestEnd = openAt, len(text)
				}
				break
			}
			closeAt := tagEnd + closeRel
			closeGt := strings.Index(text[closeAt:], ">")
			if closeGt < 0 {
				break
			}
			end := closeAt + closeGt + 1
			if bestStart < 0 || openAt < bestStart {
				bestStart, bestEnd = openAt, end
			}
			break
		}
	}
	if bestStart < 0 {
		return text
	}
	return strings.TrimSpace(text[:bestStart]) + "\n\n" + strings.TrimSpace(text[bestEnd:])
}

func buildPrioritizedScanText(segments []string) (scanText string, metadataText string) {
	metadataText = strings.Join(segments, "\n\n")
	if len(segments) <= 1 {
		return metadataText, metadataText
	}
	return segments[0] + promptAuditPrioritySeparator + strings.Join(segments[1:], "\n\n"), metadataText
}

func RedactPreview(value string, maxRunes int) string {
	value = bearerPattern.ReplaceAllString(value, "Bearer ***")
	value = apiKeyPattern.ReplaceAllStringFunc(value, func(match string) string {
		if index := strings.IndexAny(match, ":= \t"); index >= 0 {
			return match[:index+1] + "***"
		}
		return "***"
	})
	value = canaryPattern.ReplaceAllString(value, "${1}***")
	value = emailPattern.ReplaceAllString(value, "***@***")
	value = phonePattern.ReplaceAllString(value, "***PHONE***")
	return TrimRunes(value, maxRunes)
}

// BuildPromptPreview stores only a short, non-recoverable head of sanitized
// input. Ordinary confidential prompts must not land nearly intact in PostgreSQL
// or the admin UI merely because no secret regex matched.
func BuildPromptPreview(value string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = DefaultPromptPreviewMaxRunes
	}
	redacted := strings.TrimSpace(RedactPreview(value, maxRunes))
	if redacted == "" {
		return ""
	}
	runes := []rune(redacted)
	hadTruncation := strings.HasSuffix(redacted, "…")
	if hadTruncation && len(runes) > 0 {
		runes = runes[:len(runes)-1]
	}
	if len(runes) == 0 {
		return "***…"
	}
	const minLengthForPartialPreview = 32
	if len(runes) < minLengthForPartialPreview {
		if hadTruncation {
			return "***…"
		}
		return "***"
	}
	keep := len(runes) / 4
	if keep > 24 {
		keep = 24
	}
	preview := string(runes[:keep]) + "***"
	if hadTruncation || keep < len(runes) {
		preview += "…"
	}
	return preview
}

// DefaultFullPromptMaxRunes bounds the existing admin-only event content.
const DefaultFullPromptMaxRunes = 65536

// BuildFullPrompt returns bounded event text for administrator review. NUL
// bytes are stripped because PostgreSQL TEXT rejects them.
func BuildFullPrompt(value string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = DefaultFullPromptMaxRunes
	}
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

// FullPromptFromScanText reconstructs display text from the worker payload.
func FullPromptFromScanText(scanText string) string {
	return BuildFullPrompt(strings.ReplaceAll(scanText, promptAuditPrioritySeparator, "\n\n"), DefaultFullPromptMaxRunes)
}

func normalizePromptClientIP(value string) string {
	parsed := net.ParseIP(strings.TrimSpace(value))
	if parsed == nil {
		return ""
	}
	return parsed.String()
}

func TrimRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
