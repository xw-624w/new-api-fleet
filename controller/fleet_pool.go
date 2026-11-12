package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// ===========================================================================
// 算力区分：卡级归属（专属 / 弹性 / 未分配）
// ===========================================================================

// FleetPools 分池统计 + 专属池按项目的拆分。
func FleetPools(c *gin.Context) {
	days := parseRangeDays(c.Query("range"), 1)
	stats, projects, err := model.GetFleetPoolStats(days)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"range_days": days,
		"pools":      stats,
		"projects":   projects,
	})
}

// FleetPoolCards 卡列表：?pool=dedicated|shared|unassigned&host_id=&project=
func FleetPoolCards(c *gin.Context) {
	pool := strings.TrimSpace(c.Query("pool"))
	project := strings.TrimSpace(c.Query("project"))
	hostId, _ := strconv.Atoi(c.DefaultQuery("host_id", "0"))
	cards, err := model.ListFleetPoolCards(pool, hostId, project)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"cards": cards, "total": len(cards)})
}

// FleetHostGpus 单机卡明细（带归属），供单机详情逐卡打标。
func FleetHostGpus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid host id")
		return
	}
	cards, err := model.ListFleetPoolCards("", id, "")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"cards": cards})
}

type poolSetRequest struct {
	Pool    string `json:"pool"`
	Project string `json:"project"`
	Team    string `json:"team"`
	Note    string `json:"note"`
}

// FleetSetGpuPool 设置单张卡的归属。
func FleetSetGpuPool(c *gin.Context) {
	hostId, err1 := strconv.Atoi(c.Param("host_id"))
	gpuIndex, err2 := strconv.Atoi(c.Param("gpu_index"))
	if err1 != nil || err2 != nil || hostId <= 0 || gpuIndex < 0 {
		common.ApiErrorMsg(c, "invalid host_id or gpu_index")
		return
	}
	var req poolSetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid payload")
		return
	}
	diff, err := model.SetFleetGpuPool(hostId, gpuIndex, req.Pool, req.Project, req.Team, req.Note, c.GetString("username"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"diff": diff})
}

// fleetBatchCard 批量设置里的一张卡。
type fleetBatchCard struct {
	HostId   int `json:"host_id"`
	GpuIndex int `json:"gpu_index"`
}

// FleetBatchGpuPool 批量打标：支持跨机器勾选，也兼容「一台机器 + 卡号数组」的老写法。
func FleetBatchGpuPool(c *gin.Context) {
	var req struct {
		HostId     int              `json:"host_id"`
		GpuIndexes []int            `json:"gpu_indexes"`
		Cards      []fleetBatchCard `json:"cards"`
		Pool       string           `json:"pool"`
		Project    string           `json:"project"`
		Team       string           `json:"team"`
		Note       string           `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid payload")
		return
	}

	cards := req.Cards
	if len(cards) == 0 && req.HostId > 0 {
		for _, idx := range req.GpuIndexes {
			cards = append(cards, fleetBatchCard{HostId: req.HostId, GpuIndex: idx})
		}
	}
	if len(cards) == 0 {
		common.ApiErrorMsg(c, "没有选择显卡")
		return
	}
	// 一次最多 500 张，防止误传超大数组把库拖住
	if len(cards) > 500 {
		common.ApiErrorMsg(c, "一次最多批量设置 500 张卡")
		return
	}

	by := c.GetString("username")
	updated := 0
	for _, card := range cards {
		if _, err := model.SetFleetGpuPool(card.HostId, card.GpuIndex, req.Pool, req.Project, req.Team, req.Note, by); err != nil {
			common.ApiErrorMsg(c, fmt.Sprintf("第 %d 台机器的 GPU%d 设置失败：%v（已成功 %d 张）", card.HostId, card.GpuIndex, err, updated))
			return
		}
		updated++
	}
	common.ApiSuccess(c, gin.H{"updated": updated})
}

// FleetGpuPoolHistory 一张卡的归属变更历史。
func FleetGpuPoolHistory(c *gin.Context) {
	hostId, err1 := strconv.Atoi(c.Param("host_id"))
	gpuIndex, err2 := strconv.Atoi(c.Param("gpu_index"))
	if err1 != nil || err2 != nil {
		common.ApiErrorMsg(c, "invalid host_id or gpu_index")
		return
	}
	rows, err := model.ListFleetGpuPoolHistory(hostId, gpuIndex, 50)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

// FleetGpuPoolRollback 回滚某张卡的归属。
func FleetGpuPoolRollback(c *gin.Context) {
	hostId, err1 := strconv.Atoi(c.Param("host_id"))
	gpuIndex, err2 := strconv.Atoi(c.Param("gpu_index"))
	if err1 != nil || err2 != nil {
		common.ApiErrorMsg(c, "invalid host_id or gpu_index")
		return
	}
	var req struct {
		HistoryId int `json:"history_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.HistoryId <= 0 {
		common.ApiErrorMsg(c, "invalid history_id")
		return
	}
	if err := model.RollbackFleetGpuPool(hostId, gpuIndex, req.HistoryId, c.GetString("username")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// FleetProjects 项目下拉候选。
func FleetProjects(c *gin.Context) {
	list, err := model.ListFleetProjects()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, list)
}

// FleetBilling 按项目分账（月度）。
func FleetBilling(c *gin.Context) {
	data, err := model.GetFleetBilling(c.Query("month"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}
