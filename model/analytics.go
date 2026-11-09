package model

import (
	"sort"
	"strconv"
)

// "服务器"在部署里有两种口径：
//   - node    : 部署节点（quota_data.node_name，多实例部署时每台实例一个名字）
//   - channel : 上游渠道（quota_data.channel_id，即算力供应商/上游服务）
const (
	ServerDimensionNode    = "node"
	ServerDimensionChannel = "channel"
)

// ServerUsageRow 单个"服务器"的算力使用汇总。
type ServerUsageRow struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Requests  int64  `json:"requests"`
	TokenUsed int64  `json:"token_used"`
	Quota     int64  `json:"quota"`
}

// GetServerUsageByNode 按部署节点汇总算力使用（数据来自 quota_data.node_name）。
func GetServerUsageByNode(startTime int64, endTime int64) ([]*ServerUsageRow, error) {
	rows := make([]*ServerUsageRow, 0)
	err := DB.Table("quota_data").
		Select("node_name AS name, COALESCE(SUM(count), 0) AS requests, " +
			"COALESCE(SUM(token_used), 0) AS token_used, COALESCE(SUM(quota), 0) AS quota").
		Where("created_at >= ? AND created_at <= ?", startTime, endTime).
		Group("node_name").
		Order("quota DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		row.Key = row.Name
	}
	return rows, nil
}

// GetServerUsageByChannel 按上游渠道汇总算力使用，并补齐渠道名称。
func GetServerUsageByChannel(startTime int64, endTime int64) ([]*ServerUsageRow, error) {
	type channelUsage struct {
		ChannelId int   `gorm:"column:channel_id"`
		Requests  int64 `gorm:"column:requests"`
		TokenUsed int64 `gorm:"column:token_used"`
		Quota     int64 `gorm:"column:quota"`
	}
	var raw []channelUsage
	err := DB.Table("quota_data").
		Select("channel_id, COALESCE(SUM(count), 0) AS requests, " +
			"COALESCE(SUM(token_used), 0) AS token_used, COALESCE(SUM(quota), 0) AS quota").
		Where("created_at >= ? AND created_at <= ?", startTime, endTime).
		Group("channel_id").
		Order("quota DESC").
		Find(&raw).Error
	if err != nil {
		return nil, err
	}

	names := make(map[int]string, len(raw))
	if len(raw) > 0 {
		ids := make([]int, 0, len(raw))
		for _, item := range raw {
			ids = append(ids, item.ChannelId)
		}
		type channelName struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		var found []channelName
		if err := DB.Table("channels").Select("id, name").Where("id IN ?", ids).Find(&found).Error; err == nil {
			for _, item := range found {
				names[item.Id] = item.Name
			}
		}
	}

	rows := make([]*ServerUsageRow, 0, len(raw))
	for _, item := range raw {
		rows = append(rows, &ServerUsageRow{
			Key:       strconv.Itoa(item.ChannelId),
			Name:      names[item.ChannelId],
			Requests:  item.Requests,
			TokenUsed: item.TokenUsed,
			Quota:     item.Quota,
		})
	}
	return rows, nil
}

// ConcurrencyPoint 一个时间桶上的并发数。
type ConcurrencyPoint struct {
	Timestamp int64 `json:"timestamp"`
	Active    int   `json:"active"`
}

// ConcurrencySummary 并发曲线与统计。
type ConcurrencySummary struct {
	BucketSeconds int64               `json:"bucket_seconds"`
	Points        []*ConcurrencyPoint `json:"points"`
	Peak          int                 `json:"peak"`
	Average       float64             `json:"average"`
	Calls         int                 `json:"calls"`
}

// GetConcurrencyTimeline 用日志区间反推并发数。
//
// 每条消费日志记录的是"完成时刻(created_at)"和"耗时秒数(use_time)"，
// 因此它隐含一个区间 [created_at-use_time, created_at]。
//
// 实现用扫描线：把区间拆成 (+1, 结束事件) 与 (-1, 结束事件)，按时间排序后
// 依次累加，每个时间桶记录"桶内出现过的最大并发"。这比"每桶取中点采样"
// 准确得多——成波次的流量在采样法下会漏掉整个波峰。
//
// average 用时间加权：窗口内所有调用占用的秒数 / 窗口秒数，
// 也就是"平均同时在跑多少个调用"。
//
// 注意：日志只在调用完成并计费后写入，被中断的流式请求可能没有日志，
// 所以这是并发数的**下限**；需要精确值要在请求链路上埋点。
func GetConcurrencyTimeline(startTime int64, endTime int64, bucketSeconds int64) (*ConcurrencySummary, error) {
	if bucketSeconds < 1 {
		bucketSeconds = 60
	}
	if endTime <= startTime {
		endTime = startTime + bucketSeconds
	}

	type spanRow struct {
		CreatedAt int64 `gorm:"column:created_at"`
		UseTime   int   `gorm:"column:use_time"`
	}
	var spans []spanRow
	// 与窗口有重叠的调用：结束时间落在窗口之后，且开始时间早于窗口终点
	err := DB.Table("logs").
		Select("created_at, use_time").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ?", startTime).
		Where("created_at - use_time <= ?", endTime).
		Find(&spans).Error
	if err != nil {
		return nil, err
	}

	type spanEvent struct {
		at    int64
		delta int
	}
	events := make([]spanEvent, 0, len(spans)*2)
	var busySeconds float64
	for _, span := range spans {
		end := span.CreatedAt
		start := end - int64(span.UseTime)
		if start >= end {
			start = end - 1
		}
		// 平均并发按窗口内的忙碌时长算，先裁剪到窗口
		clippedStart, clippedEnd := start, end
		if clippedStart < startTime {
			clippedStart = startTime
		}
		if clippedEnd > endTime {
			clippedEnd = endTime
		}
		if clippedEnd > clippedStart {
			busySeconds += float64(clippedEnd - clippedStart)
		}
		events = append(events, spanEvent{at: start, delta: 1}, spanEvent{at: end, delta: -1})
	}
	// 同一时刻先处理结束(-1)再处理开始(+1)，避免边界处多算一个
	sort.Slice(events, func(i, j int) bool {
		if events[i].at == events[j].at {
			return events[i].delta < events[j].delta
		}
		return events[i].at < events[j].at
	})

	totalBuckets := int((endTime-startTime)/bucketSeconds) + 1
	if totalBuckets < 1 {
		totalBuckets = 1
	}
	points := make([]*ConcurrencyPoint, 0, totalBuckets)
	active, peak, next := 0, 0, 0
	for i := 0; i < totalBuckets; i++ {
		bucketStart := startTime + int64(i)*bucketSeconds
		bucketEnd := bucketStart + bucketSeconds
		bucketMax := active
		for next < len(events) && events[next].at < bucketEnd {
			active += events[next].delta
			next++
			if active > bucketMax {
				bucketMax = active
			}
		}
		if bucketMax > peak {
			peak = bucketMax
		}
		points = append(points, &ConcurrencyPoint{Timestamp: bucketStart, Active: bucketMax})
	}

	average := 0.0
	if window := float64(endTime - startTime); window > 0 {
		average = busySeconds / window
	}
	return &ConcurrencySummary{
		BucketSeconds: bucketSeconds,
		Points:        points,
		Peak:          peak,
		Average:       average,
		Calls:         len(spans),
	}, nil
}
