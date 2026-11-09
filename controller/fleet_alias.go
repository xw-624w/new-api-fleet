package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// FleetAppAliases 列出人工别名。
func FleetAppAliases(c *gin.Context) {
	rows, err := model.ListFleetAppAliases()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

// FleetUpsertAppAlias 新增/更新别名：把容器/镜像/服务映射成业务名。
func FleetUpsertAppAlias(c *gin.Context) {
	var req struct {
		MatchType   string `json:"match_type"`
		MatchValue  string `json:"match_value"`
		DisplayName string `json:"display_name"`
		Owner       string `json:"owner"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "请求参数错误")
		return
	}

	req.MatchType = strings.TrimSpace(req.MatchType)
	req.MatchValue = strings.TrimSpace(req.MatchValue)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.MatchType == "" || req.MatchValue == "" || req.DisplayName == "" {
		common.ApiErrorMsg(c, "match_type / match_value / display_name 不能为空")
		return
	}
	valid := map[string]bool{"service": true, "image": true, "name": true}
	if !valid[req.MatchType] {
		common.ApiErrorMsg(c, "match_type 只能是 service / image / name")
		return
	}
	// 列宽限制：match_value varchar(255)、display_name/owner varchar(160)/(64)。
	// 不校验会把数据库原始报错抛给前端。
	if len([]rune(req.MatchValue)) > 255 {
		common.ApiErrorMsg(c, "match_value 最多 255 个字符")
		return
	}
	if len([]rune(req.DisplayName)) > 160 {
		common.ApiErrorMsg(c, "display_name 最多 160 个字符")
		return
	}
	req.Owner = strings.TrimSpace(req.Owner)
	if len([]rune(req.Owner)) > 64 {
		common.ApiErrorMsg(c, "owner 最多 64 个字符")
		return
	}

	row := model.FleetAppAlias{
		MatchType: req.MatchType, MatchValue: req.MatchValue,
		DisplayName: req.DisplayName, Owner: req.Owner,
		UpdatedBy: c.GetString("username"),
	}
	if err := model.UpsertFleetAppAlias(&row); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, row)
}

// FleetDeleteAppAlias 删除别名。
func FleetDeleteAppAlias(c *gin.Context) {
	var req struct {
		Id         int    `json:"id"`
		MatchType  string `json:"match_type"`
		MatchValue string `json:"match_value"`
	}
	_ = c.ShouldBindJSON(&req)
	if err := model.DeleteFleetAppAlias(req.Id, req.MatchType, req.MatchValue); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
