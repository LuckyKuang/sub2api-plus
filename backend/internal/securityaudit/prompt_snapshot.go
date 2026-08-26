package securityaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

// ExtractBlockingPromptSnapshot builds the synchronous guard input. Blocking
// always scans only the latest user text; the latestTurnOnly argument is kept
// for call-site compatibility and is ignored. Asynchronous auditing always
// uses ExtractPromptSnapshot so the complete transcript is retained for review.
func ExtractBlockingPromptSnapshot(req Request, latestTurnOnly bool) (PromptSnapshot, error) {
	_ = latestTurnOnly
	snapshot, _, err := extractPromptSnapshotWithDiagnostics(req, true)
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
	extracted := promptSegmentsFromAuditContent(document, latestTurnOnly)
	var segments []string
	if latestTurnOnly {
		segments = blockingSegmentsLatestUser(extracted)
	} else {
		segments = normalizeSegmentsLatestUserFirst(extracted)
	}
	if len(segments) == 0 {
		return PromptSnapshot{}, diagnostic, ErrNoPromptText
	}
	scanText, metadataText := buildPrioritizedScanText(segments)
	digest := sha256.Sum256([]byte(metadataText))
	stage := strings.TrimSpace(req.Stage)
	if stage == "" {
		stage = "http"
	}
	return PromptSnapshot{
		RequestID: req.RequestID, UserID: req.UserID, UsernameSnapshot: req.Username,
		UserEmailSnapshot: req.UserEmail, APIKeyID: req.APIKeyID, APIKeyNameSnapshot: req.APIKeyName,
		GroupID: cloneInt64Ptr(req.GroupID), GroupName: req.GroupName, Provider: req.Provider,
		Endpoint: req.Endpoint, Protocol: req.Protocol, Model: req.Model,
		PromptHash: hex.EncodeToString(digest[:]), RedactedPreview: BuildPromptPreview(metadataText, DefaultPromptPreviewMaxRunes),
		FullPrompt:   BuildFullPrompt(metadataText, DefaultFullPromptMaxRunes),
		PromptLength: utf8.RuneCountInString(metadataText), MessageCount: len(segments), Stage: stage,
		ScanText: scanText, BodyBytes: len(req.Body),
	}, diagnostic, nil
}

func promptSegmentsFromAuditContent(document auditcontent.Document, latestTurnOnly bool) []promptSegment {
	segments := make([]promptSegment, 0, len(document.Segments))
	for _, segment := range document.Segments {
		if !isPromptAuditConversationSegment(segment, latestTurnOnly) {
			continue
		}
		role := segment.Role
		user := role == "user"
		if role == "" && (segment.Source == auditcontent.SourceMessage ||
			segment.Source == auditcontent.SourceSearchQuery ||
			segment.Source == auditcontent.SourceEmbeddingInput ||
			segment.Source == auditcontent.SourceMediaPrompt) {
			user = true
			role = "user"
		}
		segments = append(segments, promptSegment{
			text: segment.Text,
			user: user,
			role: role,
		})
	}
	return segments
}

func isPromptAuditConversationSegment(segment auditcontent.Segment, latestTurnOnly bool) bool {
	if latestTurnOnly {
		switch segment.Source {
		case auditcontent.SourceMessage:
			// Keep assistant/model messages as turn separators so older user
			// text is not joined with the latest user turn. They are not emitted.
			return true
		case auditcontent.SourceSearchQuery, auditcontent.SourceEmbeddingInput, auditcontent.SourceMediaPrompt:
			return true
		default:
			return false
		}
	}
	switch segment.Source {
	case auditcontent.SourceMessage, auditcontent.SourceInstruction,
		auditcontent.SourceSearchQuery, auditcontent.SourceEmbeddingInput,
		auditcontent.SourceMediaPrompt, auditcontent.SourcePromptVariable,
		auditcontent.SourceReasoning:
		return true
	default:
		return false
	}
}

// DefaultPromptPreviewMaxRunes caps how much sanitized prompt text may be
// considered before BuildPromptPreview withholds the majority for storage/UI.
const DefaultPromptPreviewMaxRunes = 96

// DefaultFullPromptMaxRunes caps how much unredacted prompt text is persisted
// on an audit event for admin review. It is deliberately generous so realistic
// prompts are kept intact while bounding per-row storage.
const DefaultFullPromptMaxRunes = 65536

func normalizeSegmentsLatestUserFirst(values []promptSegment) []string {
	normalized := normalizedPromptSegments(values)
	if len(normalized) == 0 {
		return nil
	}
	priorityIndex := len(normalized) - 1
	for index := len(normalized) - 1; index >= 0; index-- {
		if isUserSegment(normalized[index]) {
			priorityIndex = index
			break
		}
	}
	result := make([]string, 0, len(normalized))
	result = append(result, normalized[priorityIndex].text)
	for index, segment := range normalized {
		if index != priorityIndex {
			result = append(result, segment.text)
		}
	}
	return result
}

// blockingSegmentsLatestUser limits synchronous guard input to the current
// user turn. Instructions, previous assistant/model output, and older user
// messages stay out of blocking so client harness text cannot trip the guard.
func blockingSegmentsLatestUser(values []promptSegment) []string {
	normalized := normalizedPromptSegments(values)
	latestUserStart := latestUserSegmentStart(normalized)
	if latestUserStart < 0 {
		return nil
	}
	latestUserEnd := latestUserStart
	for latestUserEnd < len(normalized) && isUserSegment(normalized[latestUserEnd]) {
		latestUserEnd++
	}
	currentUserText := make([]string, 0, latestUserEnd-latestUserStart)
	for _, segment := range normalized[latestUserStart:latestUserEnd] {
		currentUserText = append(currentUserText, segment.text)
	}
	return []string{strings.Join(currentUserText, "\n\n")}
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

// BuildFullPrompt returns the complete prompt text for audit-event storage and
// admin review, without redaction. NUL bytes are stripped because PostgreSQL
// TEXT rejects them, and the result is capped at maxRunes.
func BuildFullPrompt(value string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = DefaultFullPromptMaxRunes
	}
	value = strings.ReplaceAll(value, "\x00", "")
	return TrimRunes(strings.TrimSpace(value), maxRunes)
}

// FullPromptFromScanText reconstructs display text from the worker payload.
func FullPromptFromScanText(scanText string) string {
	return BuildFullPrompt(strings.ReplaceAll(scanText, promptAuditPrioritySeparator, "\n\n"), DefaultFullPromptMaxRunes)
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
