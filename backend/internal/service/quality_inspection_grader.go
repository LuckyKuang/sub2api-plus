package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"regexp"
	"strings"
)

// 判题只认显式结论，避免把推理过程中的数字当成答案。
var (
	qualityFencePattern  = regexp.MustCompile("(?s)```.*?```")
	qualityMarkerPattern = regexp.MustCompile(`最终答案\s*[:：]\s*([^\n]*)`)
	qualityBoxedPattern  = regexp.MustCompile(`\\boxed\{\s*([^{}]+?)\s*\}`)
	qualityNumberPattern = regexp.MustCompile(`^[-+]?(?:\d+(?:\.\d+)?|\.\d+)(?:/\d+)?`)
	qualityMarkdownStrip = strings.NewReplacer("*", "", "`", "", "_", "", "#", "")
)

// QuestionVersion 计算题目内容版本：题干/答案/次数任何改动都会改变版本。
func QuestionVersion(q QualityQuestion) string {
	payload := struct {
		ID           string   `json:"id"`
		Title        string   `json:"title"`
		Kind         string   `json:"kind"`
		Prompt       string   `json:"prompt"`
		Instructions string   `json:"instructions"`
		Expected     string   `json:"expected"`
		Choices      []string `json:"choices"`
		Unit         string   `json:"unit"`
		Rounds       int      `json:"rounds"`
	}{q.ID, q.Title, q.Kind, q.Prompt, q.Instructions, q.Expected, q.Choices, q.Unit, q.Rounds}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

// GradeQualityQuestion 返回单题判定与识别到的答案。
// reference 恒为 reference；drawing 只看 SVG 完整性。
func GradeQualityQuestion(q QualityQuestion, response string) (string, string) {
	switch q.Kind {
	case QualityKindReference:
		return QualityStatusReference, ""
	case QualityKindDrawing:
		lower := strings.ToLower(response)
		if strings.Contains(lower, "<svg") && strings.Contains(lower, "</svg") {
			return QualityStatusPass, ""
		}
		return QualityStatusFail, ""
	case QualityKindJSON:
		return gradeQualityJSON(q, response)
	case QualityKindChoice:
		value, ok := qualityAnswerSlot(response)
		if !ok {
			return QualityStatusUnknown, ""
		}
		value = strings.TrimRight(value, "。.!！ ")
		if len([]rune(value)) != 1 || !containsString(q.Choices, value) {
			return QualityStatusUnknown, ""
		}
		if value == q.Expected {
			return QualityStatusPass, value
		}
		return QualityStatusFail, value
	default:
		return gradeQualityNumber(q, response)
	}
}

func gradeQualityNumber(q QualityQuestion, response string) (string, string) {
	value, ok := qualityAnswerSlot(response)
	if !ok {
		return QualityStatusUnknown, ""
	}
	trimmed, ok := stripQualityUnit(value, q.Unit)
	if !ok {
		return QualityStatusUnknown, ""
	}
	actual, okActual := new(big.Rat).SetString(trimmed)
	expected, okExpected := new(big.Rat).SetString(strings.TrimSpace(q.Expected))
	if !okActual || !okExpected {
		return QualityStatusUnknown, ""
	}
	if actual.Cmp(expected) == 0 {
		return QualityStatusPass, trimmed
	}
	return QualityStatusFail, trimmed
}

func gradeQualityJSON(q QualityQuestion, response string) (string, string) {
	actual, ok := decodeQualityJSON(response)
	if !ok {
		return QualityStatusUnknown, ""
	}
	expected, ok := decodeQualityJSON(q.Expected)
	if !ok {
		return QualityStatusUnknown, ""
	}
	if reflect.DeepEqual(actual, expected) {
		return QualityStatusPass, strings.TrimSpace(response)
	}
	return QualityStatusFail, strings.TrimSpace(response)
}

func decodeQualityJSON(text string) (any, bool) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(text)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	// 拒绝尾随内容，例如 JSON 后跟解释。
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, false
	}
	return value, true
}

// qualityAnswerSlot 提取显式答案：优先“最终答案：”，其次 \boxed{}，最后单行裸答案。
func qualityAnswerSlot(response string) (string, bool) {
	text := qualityFencePattern.ReplaceAllString(response, "")
	text = qualityMarkdownStrip.Replace(text)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	if matches := qualityMarkerPattern.FindAllStringSubmatch(text, -1); len(matches) > 0 {
		value := strings.TrimSpace(matches[len(matches)-1][1])
		if value == "" {
			return "", false
		}
		if boxed := qualityBoxedPattern.FindStringSubmatch(value); len(boxed) == 2 {
			value = strings.TrimSpace(boxed[1])
		}
		return value, true
	}
	if boxed := qualityBoxedPattern.FindAllStringSubmatch(text, -1); len(boxed) > 0 {
		return strings.TrimSpace(boxed[len(boxed)-1][1]), true
	}
	if strings.Contains(text, "\n") {
		return "", false
	}
	return text, true
}

// stripQualityUnit 只接受“数字 + 可选单位 + 句末标点”，其余一律视为未识别。
func stripQualityUnit(value, unit string) (string, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, "。.！!；; ")
	number := qualityNumberPattern.FindString(value)
	if number == "" {
		return "", false
	}
	rest := strings.TrimSpace(value[len(number):])
	rest = strings.TrimSpace(strings.TrimRight(rest, "。.！!；; "))
	if rest == "" {
		return number, true
	}
	units := qualityUnitAliases(unit)
	for _, alias := range units {
		if rest == alias {
			return number, true
		}
	}
	// 允许“最终答案：21，因为…”这类补充说明，但说明里不能再出现数字，
	// 否则无法确认哪个才是结论。
	if strings.ContainsAny(rest, "0123456789") {
		return "", false
	}
	return number, true
}

func qualityUnitAliases(unit string) []string {
	switch unit {
	case "个":
		return []string{"个", "颗", "粒", "个糖果", "颗糖果", "粒糖果"}
	case "元":
		return []string{"元", "人民币", "RMB"}
	case "小时":
		return []string{"小时", "时", "h"}
	case "":
		return nil
	default:
		return []string{unit}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// AggregateQualityVerdict 聚合轮次内各题判定，优先级见方案 §8.1。
func AggregateQualityVerdict(statuses []string) string {
	if len(statuses) == 0 {
		return QualityStatusError
	}
	present := make(map[string]bool, len(statuses))
	for _, status := range statuses {
		present[status] = true
	}
	for _, state := range []string{QualityStatusError, QualityStatusQuota, QualityStatusFail,
		QualityStatusUnknown, QualityStatusReference} {
		if present[state] {
			return state
		}
	}
	return QualityStatusPass
}

// BuildQualityPrompt 把系统指令并入用户题面：探测接口只接受单条 prompt。
func BuildQualityPrompt(q QualityQuestion) string {
	instructions := strings.TrimSpace(q.Instructions)
	prompt := strings.TrimSpace(q.Prompt)
	if instructions == "" {
		return prompt
	}
	return fmt.Sprintf("%s\n\n%s", instructions, prompt)
}
