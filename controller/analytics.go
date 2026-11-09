package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// analyticsWindow 解析 start_timestamp / end_timestamp，缺省取最近 24 小时。
func analyticsWindow(c *gin.Context) (int64, int64) {
	now := common.GetTimestamp()
	endTime, _ := strconv.ParseInt(c.DefaultQuery("end_timestamp", ""), 10, 64)
	if endTime <= 0 {
		endTime = now
	}
	startTime, _ := strconv.ParseInt(c.DefaultQuery("start_timestamp", ""), 10, 64)
	if startTime <= 0 || startTime >= endTime {
		startTime = endTime - 86400
	}
	return startTime, endTime
}

// GetComputeServerUsage 按"服务器"维度（部署节点 / 上游渠道）汇总算力使用。
// 维度切换用 dimension=node|channel。
func GetComputeServerUsage(c *gin.Context) {
	startTime, endTime := analyticsWindow(c)
	dimension := c.DefaultQuery("dimension", model.ServerDimensionNode)

	var (
		rows []*model.ServerUsageRow
		err  error
	)
	switch dimension {
	case model.ServerDimensionChannel:
		rows, err = model.GetServerUsageByChannel(startTime, endTime)
	default:
		dimension = model.ServerDimensionNode
		rows, err = model.GetServerUsageByNode(startTime, endTime)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var requests, tokenUsed, quota int64
	for _, row := range rows {
		requests += row.Requests
		tokenUsed += row.TokenUsed
		quota += row.Quota
	}

	common.ApiSuccess(c, gin.H{
		"dimension":       dimension,
		"start_timestamp": startTime,
		"end_timestamp":   endTime,
		"rows":            rows,
		"totals": gin.H{
			"requests":   requests,
			"token_used": tokenUsed,
			"quota":      quota,
		},
	})
}

// GetComputeConcurrency 返回并发曲线（由日志区间反推）。
// bucket 单位秒，缺省 60；点数过多时会自动放大桶宽。
func GetComputeConcurrency(c *gin.Context) {
	startTime, endTime := analyticsWindow(c)
	bucket, _ := strconv.ParseInt(c.DefaultQuery("bucket", "60"), 10, 64)
	if bucket < 10 {
		bucket = 10
	}
	if span := endTime - startTime; span/bucket > 360 {
		bucket = span / 360
		if bucket < 10 {
			bucket = 10
		}
	}

	summary, err := model.GetConcurrencyTimeline(startTime, endTime, bucket)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}
