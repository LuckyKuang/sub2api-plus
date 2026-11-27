package service

// 内置题库：不可删除，可在计划内覆盖题干与标准答案。
// 默认每道题只请求 1 次；需要复核稳定性时把 rounds 调到 2–3。
var qualityStandardAnswerInstructions = "请独立解答题目。可以先解释推理，最后一行严格使用“最终答案：N”，其中 N 为你的整数答案。"

func BuiltinQualityQuestions() []QualityQuestion {
	candy := QualityQuestion{
		ID:    "candy",
		Title: "糖果题",
		Kind:  QualityKindNumber,
		Prompt: "在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。" +
			"现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？" +
			"（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求） " +
			"苹果味 桃子味 西瓜味 圆形 7 9 8 五角星形 7 6 4",
		Instructions: qualityStandardAnswerInstructions,
		Expected:     "21",
		Unit:         "个",
		Rounds:       1,
		Enabled:      true,
	}
	logic := QualityQuestion{
		ID:    "logic-choice",
		Title: "逻辑选择题",
		Kind:  QualityKindChoice,
		Prompt: "有 3 个盒子，只有一个盒子里有球。甲说球在 1 号，乙说球不在 1 号，丙说球在 2 号。" +
			"已知三人中只有一人说真话，球在哪个盒子？最后一行严格使用“最终答案：X”，X 只写 1、2 或 3。",
		Instructions: "请先推理再用最后一行给出结论。",
		Expected:     "3",
		Choices:      []string{"1", "2", "3"},
		Rounds:       1,
		Enabled:      true,
	}
	numeric := QualityQuestion{
		ID:           "numeric",
		Title:        "数值计算",
		Kind:         QualityKindNumber,
		Prompt:       "计算 17 × 19。最后一行严格使用“最终答案：N”，N 为整数。",
		Instructions: "请给出计算过程。",
		Expected:     "323",
		Rounds:       1,
		Enabled:      true,
	}
	jsonFollow := QualityQuestion{
		ID:           "json-contract",
		Title:        "JSON 指令遵循",
		Kind:         QualityKindJSON,
		Prompt:       `只返回一个 JSON 对象，不要解释、Markdown 或额外字符：{"answer":42,"unit":"items"}`,
		Instructions: "严格只返回题目指定的 JSON 对象。",
		Expected:     `{"answer":42,"unit":"items"}`,
		Rounds:       1,
		Enabled:      true,
	}
	fingerprint := QualityQuestion{
		ID:    "fingerprint",
		Title: "指纹观察",
		Kind:  QualityKindReference,
		Prompt: "请简要描述你的知识截止时间，并按顺序解释两种不同的推理检验方法。" +
			"不要猜测未向你提供的运行环境信息。",
		Instructions: "这是参考观察题，不参与通过率统计。",
		Rounds:       1,
		Enabled:      true,
	}
	pelican := QualityQuestion{
		ID:     "pelican",
		Title:  "鹈鹕骑自行车",
		Kind:   QualityKindDrawing,
		Prompt: "创建一个HTML，内容是SVG绘制一只鹈鹕骑自行车的2D动画",
		Instructions: "先用一小段自然语言简述实现方案，然后给出完整、可独立打开的 HTML 源代码。" +
			"不要调用工具，不要运行测试。",
		Rounds:  1,
		Enabled: true,
	}
	return []QualityQuestion{candy, logic, numeric, jsonFollow, fingerprint, pelican}
}

// BuiltinQualityQuestion 按 ID 返回内置题模板。
func BuiltinQualityQuestion(id string) (QualityQuestion, bool) {
	for _, q := range BuiltinQualityQuestions() {
		if q.ID == id {
			return q, true
		}
	}
	return QualityQuestion{}, false
}
