package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// ===========================================================================
// 算力服务（fleet）：采集上报 + 查询 + 人工台账
// 权限：除 ingest 外全部走 middleware.AdminAuth()
// ===========================================================================

// FleetIngest 采集端上报入口，使用每台机器独立的 agent token。
// 注意：这是机器接口，鉴权失败要返回真实 HTTP 状态码，
// 否则老版本采集端只看状态码会误以为推送成功（静默丢数据）。
func FleetIngest(c *gin.Context) {
	token := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer"))
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "missing agent token"})
		return
	}
	agent, err := model.FindFleetAgentByToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "invalid agent token"})
		return
	}

	var rep model.FleetReport
	if err := c.ShouldBindJSON(&rep); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid report payload"})
		return
	}
	if strings.TrimSpace(rep.Hostname) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "hostname is required"})
		return
	}
	// 拒绝严重漂移的时间戳（>10 分钟），避免客户端时钟错乱污染时序
	if rep.Ts > 0 && abs64(rep.Ts-time.Now().Unix()) > 600 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "report timestamp out of range"})
		return
	}

	host, err := model.FleetIngest(agent, &rep)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"host_id": host.Id, "ts": time.Now().Unix()})
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func FleetSummary(c *gin.Context) {
	// ?range=1d|7d|14d|29d，默认当天；?pool=dedicated|shared|unassigned 只看该池
	days := parseRangeDays(c.Query("range"), 1)
	data, err := model.GetFleetSummary(days, c.Query("pool"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}

// parseRangeDays 把 "7d" / "7" 解析成天数，非法值回退默认。
// 上限 92 天：原始样本只保留 30 天，再大的窗口也查不到东西，还会拖慢查询。
func parseRangeDays(raw string, def int) int {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return def
	}
	raw = strings.TrimSuffix(raw, "d")
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > 92 {
		n = 92
	}
	return n
}

func FleetHosts(c *gin.Context) {
	hosts, err := model.ListFleetHosts()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, hosts)
}

func FleetHostDetail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid host id")
		return
	}
	data, err := model.FleetHostDetail(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}

// FleetSeries 趋势序列。range 支持 1d/7d/14d/29d，也可传 start/end 时间戳。
func FleetSeries(c *gin.Context) {
	groupBy := c.DefaultQuery("group_by", "app")
	now := time.Now().Unix()

	var start, end int64
	if s, e := c.Query("start"), c.Query("end"); s != "" && e != "" {
		start, _ = strconv.ParseInt(s, 10, 64)
		end, _ = strconv.ParseInt(e, 10, 64)
	} else {
		days := 1
		if r := c.Query("range"); r != "" {
			days, _ = strconv.Atoi(strings.TrimSuffix(r, "d"))
		}
		if days <= 0 || days > 90 {
			days = 1
		}
		end = now
		start = now - int64(days)*86400
	}
	if end <= start {
		common.ApiErrorMsg(c, "invalid range")
		return
	}

	// 分桶粒度：1 天按 60s，多天统一按天聚合（按服务器本地时区切日）。
	// 多天看的是长期趋势，一天一个点最清楚；细粒度留给 1 天视图。
	span := end - start
	step := int64(86400)
	if span <= 86400 {
		step = 60
	}
	_ = now

	data, err := model.FleetSeries(groupBy, start, end, step)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}

func FleetApps(c *gin.Context) {
	topN, _ := strconv.Atoi(c.DefaultQuery("top", "10"))
	// ?range=1d|7d|29d &host_id=0(全部)|机器ID
	days := parseRangeDays(c.Query("range"), 1)
	hostId, _ := strconv.Atoi(c.DefaultQuery("host_id", "0"))
	apps, err := model.ListFleetApps(topN, days, hostId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, apps)
}

// FleetUpdateMeta 维护人工台账的单个字段集（PATCH 语义）。
func FleetUpdateMeta(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid host id")
		return
	}
	var patch model.FleetMetaPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		common.ApiErrorMsg(c, "invalid payload")
		return
	}
	diff, err := model.UpsertFleetMeta(id, patch, c.GetString("username"), "ui")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"diff": diff})
}

func FleetMetaHistory(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid host id")
		return
	}
	rows, err := model.ListFleetMetaHistory(id, 50)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func FleetMetaRollback(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid host id")
		return
	}
	var req struct {
		HistoryId int `json:"history_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.HistoryId <= 0 {
		common.ApiErrorMsg(c, "invalid history_id")
		return
	}
	if err := model.RollbackFleetMeta(id, req.HistoryId, c.GetString("username")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// FleetImportMeta 批量导入台账，默认 dry-run 只返回差异。
func FleetImportMeta(c *gin.Context) {
	var req struct {
		DryRun *bool                       `json:"dry_run"`
		Items  []model.FleetMetaImportItem `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid payload")
		return
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	res, err := model.ImportFleetMeta(req.Items, c.GetString("username"), dryRun)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, res)
}

// FleetCreateAgent 生成某台机器的采集 token（明文只返回一次）。
func FleetCreateAgent(c *gin.Context) {
	var req struct {
		Hostname string `json:"hostname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Hostname) == "" {
		common.ApiErrorMsg(c, "hostname is required")
		return
	}
	token, agent, err := model.CreateFleetAgent(strings.TrimSpace(req.Hostname))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"hostname": agent.Hostname,
		"token":    token,
		"note":     "保存此 token，页面不会再显示明文",
	})
}
