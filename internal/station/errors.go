package station

import (
	"fmt"

	"example.com/paleo-prep-station/internal/contracts"
)

func notFoundSpecimen(id string) error {
	return contracts.NotFound("specimen_not_found", "未找到标本 "+id)
}

func notFoundPlan(id string) error {
	return contracts.NotFound("plan_not_found", "未找到修复方案 "+id)
}

func notFoundProgress(planID string, sequence int) error {
	return contracts.NotFound(
		"progress_not_found",
		fmt.Sprintf("未找到方案 %s 的第 %d 条进度记录", planID, sequence),
	)
}

func notFoundEvidence(id string) error {
	return contracts.NotFound("evidence_not_found", "未找到证据记录 "+id)
}

func notFoundRelease(specimenID string) error {
	return contracts.NotFound("release_not_found", "标本 "+specimenID+" 尚未办理放行")
}
