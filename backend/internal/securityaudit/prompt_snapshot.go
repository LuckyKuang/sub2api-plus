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
	ErrNoPromptText         = errors.New("prompt audit request contains no user text")
	ErrPromptContentExtract = errors.New("prompt audit content-bearing request produced no auditable text")

	bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+\-/]+=*`)
	apiKeyPattern = regexp.MustCompile(`(?i)\b(sk|rk|pk|api[_-]?key|token|secret|password)[-_:=\s]+[A-Za-z0-9._~+\-/]{8,}`)
	canaryPattern = regexp.MustCompile(`(?i)([A-Z]+_CANARY_)[A-Za-z0-9_-]+`)
	emailPattern  = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	phonePattern  = regexp.MustCompile(`(?:\+?\d[\d\s().-]{8,}\d)`)
)

const promptAuditPrioritySeparator = "\x00SUB2API_PROMPT_AUDIT_PRIORITY_END\x00"

type promptSegment struct {
	text             string
	user             bool
	role             string
	source           auditcontent.Source
	current          bool
	clientControlled bool
}

func ExtractPromptSnapshot(req Request) (PromptSnapshot, error) {
	return extractPromptSnapshot(req, false)
}

// ExtractBlockingPromptSnapshot builds the narrow, low-latency blocking input
// when configured. Asynchronous auditing always uses ExtractPromptSnapshot so
// the complete client-controlled transcript is retained for review.
func ExtractBlockingPromptSnapshot(req Request, latestTurnOnly bool) (PromptSnapshot, error) {
	return extractPromptSnapshot(req, latestTurnOnly)
}

func extractPromptSnapshot(req Request, latestTurnOnly bool) (PromptSnapshot, error) {
	document, err := auditcontent.Extract(req.Protocol, req.Body)
	if err != nil {
		return PromptSnapshot{}, errors.New("prompt audit request JSON is invalid")
	}
	if document.Incomplete {
		return PromptSnapshot{}, ErrPromptContentExtract
	}
	extracted := promptSegmentsFromAuditContent(document)
	segments := normalizeSegmentsLatestUserFirst(extracted)
	if latestTurnOnly {
		segments = blockingSegmentsLatestUserAndPreviousOutput(extracted)
	}
	if len(segments) == 0 {
		if document.ContentBearing && len(document.Images) == 0 {
			return PromptSnapshot{}, ErrPromptContentExtract
		}
		return PromptSnapshot{}, ErrNoPromptText
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
		ScanText: scanText,
	}, nil
}

func promptSegmentsFromAuditContent(document auditcontent.Document) []promptSegment {
	segments := make([]promptSegment, 0, len(document.Segments))
	for _, segment := range document.Segments {
		segments = append(segments, promptSegment{
			text:             segment.Text,
			user:             segment.Role == "user" || segment.Current && segment.ClientControlled,
			role:             segment.Role,
			source:           segment.Source,
			current:          segment.Current,
			clientControlled: segment.ClientControlled,
		})
	}
	return segments
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
	priorityIndex := latestPrioritySegment(normalized, false)
	if priorityIndex < 0 {
		priorityIndex = latestPrioritySegment(normalized, true)
	}
	if priorityIndex < 0 {
		priorityIndex = len(normalized) - 1
		for index := len(normalized) - 1; index >= 0; index-- {
			if isUserSegment(normalized[index]) {
				priorityIndex = index
				break
			}
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

// blockingSegmentsLatestUserAndPreviousOutput limits synchronous guard input to
// the current client-controlled turn and the nearest preceding assistant/model
// turn. Full transcript scanning remains the default when narrowing is disabled.
func blockingSegmentsLatestUserAndPreviousOutput(values []promptSegment) []string {
	normalized := normalizedPromptSegments(values)
	if current := currentClientSegmentsWithPreviousOutput(normalized); len(current) > 0 {
		return current
	}
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
	selected = appendPreviousAssistantOutput(selected, normalized, latestUserStart)
	return promptSegmentTexts(selected)
}

func latestPrioritySegment(values []promptSegment, includeContext bool) int {
	for index := len(values) - 1; index >= 0; index-- {
		segment := values[index]
		if !segment.current || !segment.clientControlled {
			continue
		}
		if !includeContext && isPromptContextSegment(segment) {
			continue
		}
		return index
	}
	return -1
}

func currentClientSegmentsWithPreviousOutput(values []promptSegment) []string {
	start, current := collectCurrentClientSegments(values, false)
	if len(current) == 0 {
		_, current = collectCurrentClientSegments(values, true)
		if len(current) == 0 {
			return nil
		}
	}
	selected := []promptSegment{{text: strings.Join(current, "\n\n"), user: true, role: "user"}}
	if start >= 0 {
		for _, segment := range values {
			if segment.current && segment.clientControlled && isPromptContextSegment(segment) {
				selected = append(selected, segment)
			}
		}
	}
	selected = appendPreviousAssistantOutput(selected, values, start)
	return promptSegmentTexts(selected)
}

func collectCurrentClientSegments(values []promptSegment, includeContext bool) (int, []string) {
	start := -1
	current := make([]string, 0, len(values))
	for index, segment := range values {
		if !segment.current || !segment.clientControlled {
			continue
		}
		if !includeContext && isPromptContextSegment(segment) {
			continue
		}
		if start < 0 {
			start = index
		}
		current = append(current, segment.text)
	}
	return start, current
}

func isPromptContextSegment(segment promptSegment) bool {
	return segment.source == auditcontent.SourceInstruction || segment.source == auditcontent.SourceToolDefinition
}

func appendPreviousAssistantOutput(selected, values []promptSegment, before int) []promptSegment {
	for index := before - 1; index >= 0; index-- {
		if !isAssistantOutputSegment(values[index]) {
			continue
		}
		start := index
		for start > 0 && isAssistantOutputSegment(values[start-1]) {
			start--
		}
		return append(selected, values[start:index+1]...)
	}
	return selected
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
