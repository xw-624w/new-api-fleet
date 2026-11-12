package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ===========================================================================
// 算力服务（fleet）：机器 / 加速卡 / 部署应用 / 时序采样 / 人工台账
//
// 设计要点：
//  1. 事实（agent 上报）与台账（人工录入）分表，agent 永不覆盖台账字段。
//  2. 最新值冗余到 fleet_gpus / fleet_apps，列表与详情查询不需要“取每组最新”的 SQL。
//  3. 历史值进 *_samples，按整数时间桶聚合，PostgreSQL 与 SQLite 行为一致。
// ===========================================================================

const (
	FleetStatusOnline  = "online"
	FleetStatusStale   = "stale"
	FleetStatusOffline = "offline"

	fleetStaleAfterSec   = 3 * 60 // 3 个采集周期（30s）
	fleetOfflineAfterSec = 15 * 60
)

// fleetTZOffset 本地时区相对 UTC 的偏移秒数（东八区 = 28800）
func fleetTZOffset() int64 {
	_, off := time.Now().Zone()
	return int64(off)
}

// fleetDayStart 当天 0 点（按服务器本地时区，不是 UTC）。
// time.Truncate(24h) 是按 UTC 截断的，在东八区会把"今天"推迟 8 小时，
// 所以这里用本地日期重组。
func fleetDayStart() int64 {
	now := time.Now()
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Unix()
}

// fleetSampleRetention 原始样本保留多久。
// 默认 30 天（够看月度），可用环境变量 FLEET_SAMPLE_RETENTION_DAYS 覆盖。
// 30 天大约 330 万行、200~400 MB；要更久建议后续做按小时 rollup。
var fleetSampleRetention = func() int64 {
	if v := os.Getenv("FLEET_SAMPLE_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return int64(n) * 24 * 3600
		}
	}
	return 30 * 24 * 3600
}()

// --- 采集端凭据 -------------------------------------------------------------

type FleetAgent struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Hostname   string `json:"hostname" gorm:"type:varchar(128);uniqueIndex;not null"`
	TokenHash  string `json:"-" gorm:"type:varchar(64);not null"`
	Enabled    bool   `json:"enabled" gorm:"not null;default:true"`
	LastSeenAt int64  `json:"last_seen_at" gorm:"bigint;default:0"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;not null"`
}

func (FleetAgent) TableName() string { return "fleet_agents" }

// --- 机器（事实） -----------------------------------------------------------

type FleetHost struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Hostname    string `json:"hostname" gorm:"type:varchar(128);uniqueIndex;not null"`
	Ip          string `json:"ip" gorm:"type:varchar(64);index"`
	Os          string `json:"os" gorm:"type:varchar(64)"`
	CpuModel    string `json:"cpu_model" gorm:"type:varchar(128)"`
	CpuCores    int    `json:"cpu_cores"`
	MemTotal    int64  `json:"mem_total" gorm:"bigint;default:0"`
	MachineUuid string `json:"machine_uuid" gorm:"type:varchar(128);index"`
	Status      string `json:"status" gorm:"type:varchar(16);index;default:'offline'"`
	FirstSeen   int64  `json:"first_seen" gorm:"bigint;default:0"`
	LastSeen    int64  `json:"last_seen" gorm:"bigint;default:0"`
	// 最新主机指标（冗余，便于列表页一次查完）
	Cpu     float64 `json:"cpu" gorm:"default:0"`
	Load1   float64 `json:"load1" gorm:"default:0"`
	MemUsed int64   `json:"mem_used" gorm:"bigint;default:0"`
	NetRx   float64 `json:"net_rx" gorm:"default:0"`
	NetTx   float64 `json:"net_tx" gorm:"default:0"`
}

func (FleetHost) TableName() string { return "fleet_hosts" }

// --- 人工台账 ---------------------------------------------------------------

type FleetHostMeta struct {
	HostId        int    `json:"host_id" gorm:"primaryKey;autoIncrement:false"`
	DisplayName   string `json:"display_name" gorm:"type:varchar(128)"`
	Room          string `json:"room" gorm:"type:varchar(64)"`
	Rack          string `json:"rack" gorm:"type:varchar(64)"`
	DeviceModel   string `json:"device_model" gorm:"type:varchar(128)"`
	FormFactor    string `json:"form_factor" gorm:"type:varchar(32)"`
	AssetNo       string `json:"asset_no" gorm:"type:varchar(64)"`
	SerialNo      string `json:"serial_no" gorm:"type:varchar(64)"`
	BmcIp         string `json:"bmc_ip" gorm:"type:varchar(128)"`
	Owner         string `json:"owner" gorm:"type:varchar(64)"`
	Usage         string `json:"usage" gorm:"type:text"`
	Env           string `json:"env" gorm:"type:varchar(32)"`
	PurchasedAt   string `json:"purchased_at" gorm:"type:varchar(16)"`
	WarrantyUntil string `json:"warranty_until" gorm:"type:varchar(16)"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint;default:0"`
	UpdatedBy     string `json:"updated_by" gorm:"type:varchar(64)"`
}

func (FleetHostMeta) TableName() string { return "fleet_host_meta" }

type FleetMetaHistory struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	HostId       int    `json:"host_id" gorm:"index"`
	ChangedAt    int64  `json:"changed_at" gorm:"bigint"`
	ChangedBy    string `json:"changed_by" gorm:"type:varchar(64)"`
	Source       string `json:"source" gorm:"type:varchar(16)"`
	DiffJson     string `json:"diff_json" gorm:"type:text"`
	SnapshotJson string `json:"snapshot_json" gorm:"type:text"`
}

func (FleetMetaHistory) TableName() string { return "fleet_meta_history" }

// --- 加速卡（最新值） -------------------------------------------------------

type FleetGpu struct {
	Id        int     `json:"id" gorm:"primaryKey;autoIncrement"`
	HostId    int     `json:"host_id" gorm:"uniqueIndex:idx_fleet_gpu_host,priority:1"`
	Vendor    string  `json:"vendor" gorm:"type:varchar(16);uniqueIndex:idx_fleet_gpu_host,priority:2"`
	GpuIndex  int     `json:"gpu_index" gorm:"uniqueIndex:idx_fleet_gpu_host,priority:3"`
	Uuid      string  `json:"uuid" gorm:"type:varchar(128)"`
	Model     string  `json:"model" gorm:"type:varchar(128)"`
	MemTotal  int64   `json:"mem_total" gorm:"bigint;default:0"`
	MigMode   string  `json:"mig_mode" gorm:"type:varchar(32)"`
	Util      float64 `json:"util" gorm:"default:0"`
	MemUsed   int64   `json:"mem_used" gorm:"bigint;default:0"`
	Temp      float64 `json:"temp" gorm:"default:0"`
	Power     float64 `json:"power" gorm:"default:0"`
	LastTs    int64   `json:"last_ts" gorm:"bigint;default:0"`
	FirstSeen int64   `json:"first_seen" gorm:"bigint;default:0"`

	// 归属（来自 fleet_gpu_pools，查询时填充，不落库）
	Pool    string `json:"pool" gorm:"-"`
	Project string `json:"project" gorm:"-"`
	Team    string `json:"team" gorm:"-"`
}

func (FleetGpu) TableName() string { return "fleet_gpus" }

// --- 应用（最新值） ---------------------------------------------------------

type FleetApp struct {
	Id             int    `json:"id" gorm:"primaryKey;autoIncrement"`
	HostId         int    `json:"host_id" gorm:"uniqueIndex:idx_fleet_app_host_name,priority:1"`
	Name           string `json:"name" gorm:"type:varchar(128);uniqueIndex:idx_fleet_app_host_name,priority:2"`
	Kind           string `json:"kind" gorm:"type:varchar(32)"`
	Image          string `json:"image" gorm:"type:varchar(255)"`
	ContainerId    string `json:"container_id" gorm:"type:varchar(64)"`
	ComposeService string `json:"compose_service" gorm:"type:varchar(128)"`
	// 这个应用“在服务什么”：探测到的模型名，或部署时 label 声明的业务名
	Detail    string  `json:"detail" gorm:"type:varchar(160)"`
	Display   string  `json:"display" gorm:"type:varchar(160)"`
	Owner     string  `json:"owner" gorm:"type:varchar(64)"`
	GpuUtil   float64 `json:"gpu_util" gorm:"default:0"`
	GpuCount  int     `json:"gpu_count" gorm:"default:0"`
	VramUsed  int64   `json:"vram_used" gorm:"bigint;default:0"`
	Estimated bool    `json:"estimated" gorm:"default:false"`
	LastTs    int64   `json:"last_ts" gorm:"bigint;default:0"`

	// 计算字段（不落库）：别名优先，其次 agent 上报的 display，最后原名
	DisplayName string `json:"display_name" gorm:"-"`
	OwnerName   string `json:"owner_name" gorm:"-"`
}

func (FleetApp) TableName() string { return "fleet_apps" }

// FleetAppAlias 人工别名：把容器/镜像/服务映射成能读懂的业务名。
// 优先级高于自动探测，容器重建后依然生效。
type FleetAppAlias struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	MatchType   string `json:"match_type" gorm:"type:varchar(16);uniqueIndex:idx_fleet_alias,priority:1"`
	MatchValue  string `json:"match_value" gorm:"type:varchar(255);uniqueIndex:idx_fleet_alias,priority:2"`
	DisplayName string `json:"display_name" gorm:"type:varchar(160);not null"`
	Owner       string `json:"owner" gorm:"type:varchar(64)"`
	UpdatedBy   string `json:"updated_by" gorm:"type:varchar(64)"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint"`
}

func (FleetAppAlias) TableName() string { return "fleet_app_aliases" }

// --- 别名解析 ---------------------------------------------------------------

func aliasKey(matchType, value string) string {
	return matchType + "\x00" + value
}

// LoadFleetAliasIndex 读取全部别名，key 为 (类型, 值)。
func LoadFleetAliasIndex() map[string]FleetAppAlias {
	var rows []FleetAppAlias
	DB.Find(&rows)
	idx := make(map[string]FleetAppAlias, len(rows))
	for _, r := range rows {
		idx[aliasKey(r.MatchType, r.MatchValue)] = r
	}
	return idx
}

// AttachAppAliases 给应用补上展示名。
// 优先级：服务名别名 > 镜像别名 > 应用名别名 > agent 上报的 label fleet.name > 原名。
func AttachAppAliases(apps []FleetApp) {
	if len(apps) == 0 {
		return
	}
	idx := LoadFleetAliasIndex()
	for i := range apps {
		a := &apps[i]
		a.DisplayName = a.Name
		a.OwnerName = a.Owner

		var hit FleetAppAlias
		found := false
		lookups := [][2]string{
			{"service", a.ComposeService},
			{"image", a.Image},
			{"name", a.Name},
		}
		for _, l := range lookups {
			if l[1] == "" {
				continue
			}
			if v, ok := idx[aliasKey(l[0], l[1])]; ok {
				hit, found = v, true
				break
			}
		}
		switch {
		case found:
			a.DisplayName = hit.DisplayName
			if hit.Owner != "" {
				a.OwnerName = hit.Owner
			}
		case a.Display != "":
			a.DisplayName = a.Display
		}
	}
}

// UpsertFleetAppAlias 新增/更新一条别名。
func UpsertFleetAppAlias(a *FleetAppAlias) error {
	a.UpdatedAt = time.Now().Unix()
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "match_type"}, {Name: "match_value"}},
		DoUpdates: clause.AssignmentColumns([]string{"display_name", "owner", "updated_by", "updated_at"}),
	}).Create(a).Error
}

// DeleteFleetAppAlias 删除一条别名（按 id 或按类型+值）。
func DeleteFleetAppAlias(id int, matchType, matchValue string) error {
	q := DB.Model(&FleetAppAlias{})
	if id > 0 {
		q = q.Where("id = ?", id)
	} else {
		q = q.Where("match_type = ? AND match_value = ?", matchType, matchValue)
	}
	return q.Delete(&FleetAppAlias{}).Error
}

// ListFleetAppAliases 返回全部别名。
func ListFleetAppAliases() ([]FleetAppAlias, error) {
	var rows []FleetAppAlias
	if err := DB.Order("match_type, match_value").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// --- 时序采样 ---------------------------------------------------------------

type FleetHostSample struct {
	Ts      int64   `json:"ts" gorm:"primaryKey;autoIncrement:false"`
	HostId  int     `json:"host_id" gorm:"primaryKey;autoIncrement:false"`
	Cpu     float64 `json:"cpu" gorm:"default:0"`
	Load1   float64 `json:"load1" gorm:"default:0"`
	MemUsed int64   `json:"mem_used" gorm:"bigint;default:0"`
	NetRx   float64 `json:"net_rx" gorm:"default:0"`
	NetTx   float64 `json:"net_tx" gorm:"default:0"`
}

func (FleetHostSample) TableName() string { return "fleet_host_samples" }

type FleetGpuSample struct {
	Ts       int64   `json:"ts" gorm:"primaryKey;autoIncrement:false"`
	HostId   int     `json:"host_id" gorm:"primaryKey;autoIncrement:false"`
	Vendor   string  `json:"vendor" gorm:"primaryKey;size:16"`
	GpuIndex int     `json:"gpu_index" gorm:"primaryKey;autoIncrement:false"`
	Util     float64 `json:"util" gorm:"default:0"`
	MemUsed  int64   `json:"mem_used" gorm:"bigint;default:0"`
	Temp     float64 `json:"temp" gorm:"default:0"`
	Power    float64 `json:"power" gorm:"default:0"`
}

func (FleetGpuSample) TableName() string { return "fleet_gpu_samples" }

type FleetAppSample struct {
	Ts        int64   `json:"ts" gorm:"primaryKey;autoIncrement:false"`
	AppId     int     `json:"app_id" gorm:"primaryKey;autoIncrement:false"`
	GpuUtil   float64 `json:"gpu_util" gorm:"default:0"`
	VramUsed  int64   `json:"vram_used" gorm:"bigint;default:0"`
	Estimated bool    `json:"estimated" gorm:"default:false"`
}

func (FleetAppSample) TableName() string { return "fleet_app_samples" }

// ===========================================================================
// 采集上报
// ===========================================================================

type FleetGpuReport struct {
	Vendor   string  `json:"vendor"`
	Index    int     `json:"index"`
	Uuid     string  `json:"uuid"`
	Model    string  `json:"model"`
	MemTotal int64   `json:"mem_total"`
	Util     float64 `json:"util"`
	MemUsed  int64   `json:"mem_used"`
	Temp     float64 `json:"temp"`
	Power    float64 `json:"power"`
	MigMode  string  `json:"mig_mode"`
}

type FleetAppReport struct {
	Name           string  `json:"name"`
	Kind           string  `json:"kind"`
	Image          string  `json:"image"`
	ContainerId    string  `json:"container_id"`
	ComposeService string  `json:"compose_service"`
	Detail         string  `json:"detail"`
	Display        string  `json:"display"`
	Owner          string  `json:"owner"`
	GpuUtil        float64 `json:"gpu_util"`
	GpuCount       int     `json:"gpu_count"`
	VramUsed       int64   `json:"vram_used"`
	Estimated      bool    `json:"estimated"`
}

type FleetReport struct {
	Hostname    string           `json:"hostname"`
	Ip          string           `json:"ip"`
	Os          string           `json:"os"`
	CpuModel    string           `json:"cpu_model"`
	CpuCores    int              `json:"cpu_cores"`
	MemTotal    int64            `json:"mem_total"`
	MachineUuid string           `json:"machine_uuid"`
	Ts          int64            `json:"ts"`
	Gpus        []FleetGpuReport `json:"gpus"`
	Apps        []FleetAppReport `json:"apps"`
	Metrics     struct {
		Cpu     float64 `json:"cpu"`
		Load1   float64 `json:"load1"`
		MemUsed int64   `json:"mem_used"`
		NetRx   float64 `json:"net_rx"`
		NetTx   float64 `json:"net_tx"`
	} `json:"host_metrics"`
}

func HashFleetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func CreateFleetAgent(hostname string) (string, *FleetAgent, error) {
	token := fmt.Sprintf("flt_%d_%s", time.Now().UnixNano(), hostname)
	agent := &FleetAgent{
		Hostname:  hostname,
		TokenHash: HashFleetToken(token),
		Enabled:   true,
		CreatedAt: time.Now().Unix(),
	}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "hostname"}},
		DoUpdates: clause.AssignmentColumns([]string{"token_hash", "enabled"}),
	}).Create(agent).Error; err != nil {
		return "", nil, err
	}
	return token, agent, nil
}

func FindFleetAgentByToken(token string) (*FleetAgent, error) {
	var agent FleetAgent
	err := DB.Where("token_hash = ? AND enabled = ?", HashFleetToken(token), true).
		First(&agent).Error
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

// FleetIngest 写入一次上报：机器、加速卡、应用、三类采样点。
func FleetIngest(agent *FleetAgent, rep *FleetReport) (*FleetHost, error) {
	ts := rep.Ts
	if ts <= 0 {
		ts = time.Now().Unix()
	}

	host, err := upsertFleetHost(agent, rep, ts)
	if err != nil {
		return nil, err
	}

	DB.Model(&FleetAgent{}).Where("id = ?", agent.Id).
		Update("last_seen_at", ts)

	if err := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&FleetHostSample{
		Ts: ts, HostId: host.Id,
		Cpu: rep.Metrics.Cpu, Load1: rep.Metrics.Load1,
		MemUsed: rep.Metrics.MemUsed, NetRx: rep.Metrics.NetRx, NetTx: rep.Metrics.NetTx,
	}).Error; err != nil {
		return nil, err
	}

	for _, g := range rep.Gpus {
		vendor := g.Vendor
		if vendor == "" {
			vendor = "nvidia"
		}
		gpu := FleetGpu{
			HostId: host.Id, Vendor: vendor, GpuIndex: g.Index,
			Uuid: g.Uuid, Model: g.Model, MemTotal: g.MemTotal, MigMode: g.MigMode,
			Util: g.Util, MemUsed: g.MemUsed, Temp: g.Temp, Power: g.Power, LastTs: ts,
		}
		if err := DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "host_id"}, {Name: "vendor"}, {Name: "gpu_index"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"uuid", "model", "mem_total", "mig_mode",
				"util", "mem_used", "temp", "power", "last_ts",
			}),
		}).Create(&gpu).Error; err != nil {
			return nil, err
		}
		if err := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&FleetGpuSample{
			Ts: ts, HostId: host.Id, Vendor: vendor, GpuIndex: g.Index,
			Util: g.Util, MemUsed: g.MemUsed, Temp: g.Temp, Power: g.Power,
		}).Error; err != nil {
			return nil, err
		}
	}

	for _, a := range rep.Apps {
		app := FleetApp{
			HostId: host.Id, Name: a.Name, Kind: a.Kind, Image: a.Image,
			ContainerId: a.ContainerId, ComposeService: a.ComposeService,
			Detail: a.Detail, Display: a.Display, Owner: a.Owner,
			GpuUtil: a.GpuUtil, GpuCount: a.GpuCount,
			VramUsed: a.VramUsed, Estimated: a.Estimated, LastTs: ts,
		}
		if err := DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "host_id"}, {Name: "name"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"kind", "image", "container_id", "compose_service",
				"detail", "display", "owner",
				"gpu_util", "gpu_count", "vram_used", "estimated", "last_ts",
			}),
		}).Create(&app).Error; err != nil {
			return nil, err
		}
		var stored FleetApp
		if err := DB.Where("host_id = ? AND name = ?", host.Id, a.Name).First(&stored).Error; err == nil {
			if err := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&FleetAppSample{
				Ts: ts, AppId: stored.Id,
				GpuUtil: a.GpuUtil, VramUsed: a.VramUsed, Estimated: a.Estimated,
			}).Error; err != nil {
				return nil, err
			}
		}
	}

	maybePruneFleetSamples()
	return host, nil
}

func upsertFleetHost(agent *FleetAgent, rep *FleetReport, ts int64) (*FleetHost, error) {
	var host FleetHost

	// 机器身份只认硬件 UUID（主板序列号），hostname 和 IP 都只是属性。
	// IP 不做身份匹配：内网 IP 会被重新分配，用 IP 认机器会把 A 的台账挂到 B 上。
	if rep.MachineUuid != "" {
		err := DB.Where("machine_uuid = ?", rep.MachineUuid).First(&host).Error
		if err == nil {
			// UUID 已知但 hostname 变了（改名）→ 仍然沿用原记录，顺手更新名字
			return updateFleetHost(&host, rep, ts)
		}
	}

	// 兼容：机器没报 UUID（老系统/容器里读不到 DMI），退回按 hostname 认领
	if err := DB.Where("hostname = ? AND (machine_uuid = '' OR machine_uuid IS NULL)", rep.Hostname).
		First(&host).Error; err == nil {
		return updateFleetHost(&host, rep, ts)
	}

	host = FleetHost{
		Hostname:    rep.Hostname,
		Ip:          rep.Ip,
		Os:          rep.Os,
		CpuModel:    rep.CpuModel,
		CpuCores:    rep.CpuCores,
		MemTotal:    rep.MemTotal,
		MachineUuid: rep.MachineUuid,
		Status:      FleetStatusOnline,
		FirstSeen:   ts,
	}
	if err := DB.Create(&host).Error; err != nil {
		return nil, err
	}
	return updateFleetHost(&host, rep, ts)
}

func updateFleetHost(host *FleetHost, rep *FleetReport, ts int64) (*FleetHost, error) {
	updates := map[string]any{
		"hostname":  rep.Hostname,
		"ip":        rep.Ip,
		"os":        rep.Os,
		"cpu":       rep.Metrics.Cpu,
		"load1":     rep.Metrics.Load1,
		"mem_used":  rep.Metrics.MemUsed,
		"net_rx":    rep.Metrics.NetRx,
		"net_tx":    rep.Metrics.NetTx,
		"status":    FleetStatusOnline,
		"last_seen": ts,
	}
	if rep.MemTotal > 0 {
		updates["mem_total"] = rep.MemTotal
	}
	if rep.CpuCores > 0 {
		updates["cpu_cores"] = rep.CpuCores
	}
	if rep.CpuModel != "" {
		updates["cpu_model"] = rep.CpuModel
	}
	if rep.MachineUuid != "" {
		updates["machine_uuid"] = rep.MachineUuid
	}
	if err := DB.Model(host).Updates(updates).Error; err != nil {
		return nil, err
	}
	return host, nil
}

// maybePruneFleetSamples 以 2% 概率触发清理，避免每次上报都跑删除语句。
func maybePruneFleetSamples() {
	if time.Now().UnixNano()%50 != 0 {
		return
	}
	cutoff := time.Now().Unix() - fleetSampleRetention
	DB.Where("ts < ?", cutoff).Delete(&FleetGpuSample{})
	DB.Where("ts < ?", cutoff).Delete(&FleetAppSample{})
	DB.Where("ts < ?", cutoff).Delete(&FleetHostSample{})
}

// RefreshFleetHostStatus 由查询入口顺带调用：长时间没上报的机器转为 stale/offline。
func RefreshFleetHostStatus() {
	now := time.Now().Unix()
	DB.Model(&FleetHost{}).
		Where("status = ? AND last_seen < ?", FleetStatusOnline, now-fleetStaleAfterSec).
		Update("status", FleetStatusStale)
	DB.Model(&FleetHost{}).
		Where("status IN ? AND last_seen < ?", []string{FleetStatusOnline, FleetStatusStale}, now-fleetOfflineAfterSec).
		Update("status", FleetStatusOffline)
}

// ===========================================================================
// 查询
// ===========================================================================

type FleetHostView struct {
	Id          int     `json:"id"`
	Hostname    string  `json:"hostname"`
	DisplayName string  `json:"display_name"`
	Ip          string  `json:"ip"`
	Room        string  `json:"room"`
	Owner       string  `json:"owner"`
	Usage       string  `json:"usage"`
	Status      string  `json:"status"`
	LastSeen    int64   `json:"last_seen"`
	Cpu         float64 `json:"cpu"`
	MemUsed     int64   `json:"mem_used"`
	MemTotal    int64   `json:"mem_total"`
	GpuCount    int     `json:"gpus"`
	GpuSummary  string  `json:"gpu_summary"`
	Util        float64 `json:"util"`
	VramUsed    int64   `json:"vram_used"`
	VramTotal   int64   `json:"vram_total"`
	TempMax     float64 `json:"temp_max"`
	AppCount    int     `json:"apps"`
	IdleCards   int     `json:"idle_cards"`
	// 该机各池卡数（专属 / 弹性 / 未分配），列表里直接展示
	PoolDedicated  int `json:"pool_dedicated"`
	PoolShared     int `json:"pool_shared"`
	PoolUnassigned int `json:"pool_unassigned"`
}

func ListFleetHosts() ([]FleetHostView, error) {
	RefreshFleetHostStatus()

	var hosts []FleetHost
	if err := DB.Order("id ASC").Find(&hosts).Error; err != nil {
		return nil, err
	}
	var metas []FleetHostMeta
	DB.Find(&metas)
	metaMap := make(map[int]FleetHostMeta, len(metas))
	for _, m := range metas {
		metaMap[m.HostId] = m
	}

	var gpus []FleetGpu
	DB.Find(&gpus)
	gpusByHost := make(map[int][]FleetGpu)
	for _, g := range gpus {
		gpusByHost[g.HostId] = append(gpusByHost[g.HostId], g)
	}

	type appCount struct {
		HostId int
		N      int
	}
	var counts []appCount
	DB.Model(&FleetApp{}).Select("host_id, count(*) as n").Group("host_id").Scan(&counts)
	appMap := make(map[int]int, len(counts))
	for _, c := range counts {
		appMap[c.HostId] = c.N
	}

	poolMap := loadFleetPoolMap()
	views := make([]FleetHostView, 0, len(hosts))
	for _, h := range hosts {
		v := FleetHostView{
			Id: h.Id, Hostname: h.Hostname, Ip: h.Ip, Status: h.Status,
			LastSeen: h.LastSeen, Cpu: h.Cpu, MemUsed: h.MemUsed, MemTotal: h.MemTotal,
			AppCount: appMap[h.Id],
		}
		if m, ok := metaMap[h.Id]; ok {
			v.DisplayName, v.Room, v.Owner, v.Usage = m.DisplayName, m.Room, m.Owner, m.Usage
		}
		gList := gpusByHost[h.Id]
		v.GpuCount = len(gList)
		var utilSum float64
		for _, g := range gList {
			utilSum += g.Util
			v.VramUsed += g.MemUsed
			v.VramTotal += g.MemTotal
			if g.Temp > v.TempMax {
				v.TempMax = g.Temp
			}
		}
		if len(gList) > 0 {
			v.Util = utilSum / float64(len(gList))
		}
		for _, g := range gList {
			if g.Util < 5 {
				v.IdleCards++
			}
		}
		for _, g := range gList {
			switch fleetPoolOf(poolMap, g.HostId, g.GpuIndex).Pool {
			case FleetPoolDedicated:
				v.PoolDedicated++
			case FleetPoolShared:
				v.PoolShared++
			default:
				v.PoolUnassigned++
			}
		}
		v.GpuSummary = summarizeFleetGpus(gList)
		views = append(views, v)
	}
	return views, nil
}

func summarizeFleetGpus(gpus []FleetGpu) string {
	if len(gpus) == 0 {
		return "—"
	}
	counts := map[string]int{}
	order := make([]string, 0, 2)
	for _, g := range gpus {
		if _, ok := counts[g.Model]; !ok {
			order = append(order, g.Model)
		}
		counts[g.Model]++
	}
	if len(order) == 1 {
		return fmt.Sprintf("%d× %s", counts[order[0]], order[0])
	}
	out := ""
	for i, m := range order {
		if i > 0 {
			out += " + "
		}
		out += fmt.Sprintf("%d×%s", counts[m], m)
	}
	return out
}

type FleetSummary struct {
	Hosts struct {
		Total  int `json:"total"`
		Online int `json:"online"`
	} `json:"hosts"`
	Gpus struct {
		Total  int `json:"total"`
		Nvidia int `json:"nvidia"`
		Intel  int `json:"intel"`
		InUse  int `json:"in_use"`
	} `json:"gpus"`
	AvgUtil       float64 `json:"avg_util"`
	VramUsed      int64   `json:"vram_used"`
	VramTotal     int64   `json:"vram_total"`
	GpuHoursToday float64 `json:"gpu_hours_today"`
	// CoveredHoursToday：今日的样本实际覆盖了几小时（服务刚启动时会小于 24）
	CoveredHoursToday float64 `json:"covered_hours_today"`
	LastIngestAt      int64   `json:"last_ingest_at"`
	Degraded          bool    `json:"degraded"`
	// 分池统计（卡级归属），老前端不认这个字段也不受影响
	Pools map[string]*FleetPoolStat `json:"pools,omitempty"`
}

// GetFleetSummary 顶部 KPI。pool 为空或 all 时是全局口径；
// 传 dedicated / shared / unassigned 时，所有指标只统计该池的卡。
func GetFleetSummary(rangeDays int, pool string) (*FleetSummary, error) {
	if rangeDays <= 0 {
		rangeDays = 1
	}
	pool = strings.TrimSpace(strings.ToLower(pool))
	if pool == "all" {
		pool = ""
	}
	if pool != "" && pool != FleetPoolDedicated && pool != FleetPoolShared && pool != FleetPoolUnassigned {
		pool = ""
	}
	RefreshFleetHostStatus()

	var s FleetSummary
	var hosts []FleetHost
	if err := DB.Find(&hosts).Error; err != nil {
		return nil, err
	}

	var gpus []FleetGpu
	if err := DB.Find(&gpus).Error; err != nil {
		return nil, err
	}

	// 按池过滤：只留该池的卡
	poolMap := loadFleetPoolMap()
	if pool != "" {
		kept := make([]FleetGpu, 0, len(gpus))
		for _, g := range gpus {
			if fleetPoolOf(poolMap, g.HostId, g.GpuIndex).Pool == pool {
				kept = append(kept, g)
			}
		}
		gpus = kept
	}

	// 主机口径：全局是所有机器；按池时只算「有该池卡」的机器
	hostHasCard := make(map[int]bool, len(hosts))
	for _, g := range gpus {
		hostHasCard[g.HostId] = true
	}
	for _, h := range hosts {
		if pool != "" && !hostHasCard[h.Id] {
			continue
		}
		s.Hosts.Total++
		if h.Status != FleetStatusOffline {
			s.Hosts.Online++
		}
		if h.LastSeen > s.LastIngestAt {
			s.LastIngestAt = h.LastSeen
		}
	}

	var utilSum float64
	for _, g := range gpus {
		s.Gpus.Total++
		if g.Vendor == "intel" {
			s.Gpus.Intel++
		} else {
			s.Gpus.Nvidia++
		}
		if g.Util >= 5 {
			s.Gpus.InUse++
		}
		utilSum += g.Util
		s.VramUsed += g.MemUsed
		s.VramTotal += g.MemTotal
	}
	if s.Gpus.Total > 0 {
		// 口径：池内每张卡一票的算术平均
		s.AvgUtil = utilSum / float64(s.Gpus.Total)
	}

	// 区间 GPU·h：对区间内采样按「利用率 × 采样间隔」积分
	// rangeDays=1 时从当天 0 点算起，大于 1 天则从 now-N天 算起
	var windowStart int64
	if rangeDays == 1 {
		windowStart = fleetDayStart()
	} else {
		windowStart = time.Now().Unix() - int64(rangeDays)*86400
	}
	todayStart := windowStart
	var agg struct {
		SumUtil  float64 `gorm:"column:sum_util"`
		Samples  int64   `gorm:"column:samples"`
		Distinct int64   `gorm:"column:distinct"`
		MinTs    int64   `gorm:"column:min_ts"`
		MaxTs    int64   `gorm:"column:max_ts"`
	}
	if pool == "" {
		DB.Model(&FleetGpuSample{}).
			Select("COALESCE(SUM(util),0) as sum_util, COUNT(*) as samples, COUNT(DISTINCT ts) as distinct, MIN(ts) as min_ts, MAX(ts) as max_ts").
			Where("ts >= ?", todayStart).Scan(&agg)
	} else {
		DB.Raw(`SELECT COALESCE(SUM(s.util),0) AS sum_util, COUNT(*) AS samples,
		               COUNT(DISTINCT s.ts) AS distinct, MIN(s.ts) AS min_ts, MAX(s.ts) AS max_ts
		        FROM fleet_gpu_samples s
		        JOIN fleet_gpu_pools p
		          ON p.host_id = s.host_id AND p.gpu_index = s.gpu_index
		        WHERE s.ts >= ? AND p.pool = ?`, todayStart, pool).Scan(&agg)
	}
	if agg.Samples > 0 {
		interval := 30.0
		if agg.MaxTs > agg.MinTs && agg.Distinct > 1 {
			interval = float64(agg.MaxTs-agg.MinTs) / float64(agg.Distinct-1)
		}
		s.GpuHoursToday = agg.SumUtil / 100 * interval / 3600
		// 今日样本实际覆盖的小时数：用来把"卡时"翻译成"平均几张卡在跑"
		if agg.MaxTs > agg.MinTs {
			s.CoveredHoursToday = float64(agg.MaxTs-agg.MinTs) / 3600
		}
	}

	s.Degraded = s.LastIngestAt == 0 || time.Now().Unix()-s.LastIngestAt > fleetOfflineAfterSec
	if pools, _, err := GetFleetPoolStats(rangeDays); err == nil {
		s.Pools = pools
	}
	return &s, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// FleetHostDetail 单机详情：机器 + 台账 + 每卡 + 每应用。
func FleetHostDetail(hostId int) (map[string]any, error) {
	var host FleetHost
	if err := DB.First(&host, hostId).Error; err != nil {
		return nil, err
	}
	var meta FleetHostMeta
	if err := DB.Where("host_id = ?", hostId).First(&meta).Error; err != nil {
		meta = FleetHostMeta{HostId: hostId}
	}
	var gpus []FleetGpu
	DB.Where("host_id = ?", hostId).Order("vendor ASC, gpu_index ASC").Find(&gpus)
	// 带上归属，单机详情里直接逐卡打标
	poolMap := loadFleetPoolMap()
	for i := range gpus {
		p := fleetPoolOf(poolMap, gpus[i].HostId, gpus[i].GpuIndex)
		gpus[i].Pool = p.Pool
		gpus[i].Project = p.Project
		gpus[i].Team = p.Team
	}
	var apps []FleetApp
	DB.Where("host_id = ?", hostId).Order("gpu_util DESC").Find(&apps)
	AttachAppAliases(apps)

	return map[string]any{
		"host": host,
		"meta": meta,
		"gpus": gpus,
		"apps": apps,
	}, nil
}

// FleetSeries 时序聚合：metric=util|vram，group_by=host|app。
func FleetSeries(groupBy string, start, end, step int64) (map[string]any, error) {
	if step <= 0 {
		step = 300
	}
	// 只有明确要 host / pool 时才按机器或池分组，其他非法值一律回退到 app，
	// 避免拼错参数后静默返回另一种维度。
	switch groupBy {
	case "host", "pool":
	default:
		groupBy = "app"
	}
	type row struct {
		Bucket int64   `json:"bucket"`
		Key    int     `json:"key"`
		Value  float64 `json:"value"`
	}
	rows := []row{}

	switch groupBy {
	case "app":
		var apps []FleetApp
		DB.Find(&apps)
		AttachAppAliases(apps)
		nameMap := make(map[int]string, len(apps))
		for _, a := range apps {
			// 趋势图用别名后的名字，避免同一应用在两处叫法不同
			nameMap[a.Id] = a.DisplayName
		}
		// 按本地时区对齐分桶：step=86400 时就是「当地 0 点~24 点」为一天，
		// 否则会按 UTC 切分，东八区的"一天"会从早上 8 点开始。
		tz := fleetTZOffset()
		DB.Raw(`SELECT ((ts + ?) / ?) * ? - ? AS bucket, app_id AS key, AVG(gpu_util) AS value
		        FROM fleet_app_samples WHERE ts >= ? AND ts <= ?
		        GROUP BY bucket, app_id ORDER BY bucket`,
			tz, step, step, tz, start, end).Scan(&rows)

		// 同名应用跨机器时合并成一条曲线，避免图表出现重复 key
		merged := map[int64]map[string]float64{}
		for _, r := range rows {
			name, ok := nameMap[r.Key]
			if !ok {
				name = "unknown"
			}
			if merged[r.Bucket] == nil {
				merged[r.Bucket] = map[string]float64{}
			}
			merged[r.Bucket][name] += r.Value
		}
		keySet := map[string]struct{}{}
		for _, perBucket := range merged {
			for name := range perBucket {
				keySet[name] = struct{}{}
			}
		}
		keys := make([]string, 0, len(keySet))
		for name := range keySet {
			keys = append(keys, name)
		}
		sort.Strings(keys)

		buckets := make([]int64, 0, len(merged))
		for b := range merged {
			buckets = append(buckets, b)
		}
		sort.Slice(buckets, func(i, j int) bool { return buckets[i] < buckets[j] })
		out := make([]map[string]any, 0, len(buckets))
		for _, b := range buckets {
			item := map[string]any{"ts": b}
			for name, v := range merged[b] {
				item[name] = v
			}
			out = append(out, item)
		}
		return map[string]any{"step": step, "keys": keys, "points": out}, nil

	case "pool":
		// 各池的利用率之和：口径与「按应用」一致，便于叠加对比
		tz := fleetTZOffset()
		var prows []struct {
			Bucket int64   `gorm:"column:bucket"`
			Key    string  `gorm:"column:pool_key"`
			Value  float64 `gorm:"column:value"`
		}
		DB.Raw(`SELECT ((s.ts + ?) / ?) * ? - ? AS bucket,
		               COALESCE(p.pool, 'unassigned') AS pool_key,
		               COALESCE(AVG(s.util), 0) AS value
		        FROM fleet_gpu_samples s
		        LEFT JOIN fleet_gpu_pools p
		               ON p.host_id = s.host_id AND p.gpu_index = s.gpu_index
		        WHERE s.ts >= ? AND s.ts <= ?
		        GROUP BY 1, 2 ORDER BY 1`, tz, step, step, tz, start, end).Scan(&prows)

		label := map[string]string{
			FleetPoolDedicated:  "专属",
			FleetPoolShared:     "弹性",
			FleetPoolUnassigned: "未分配",
		}
		points := map[int64]map[string]float64{}
		for _, r := range prows {
			name := label[r.Key]
			if name == "" {
				name = r.Key
			}
			if points[r.Bucket] == nil {
				points[r.Bucket] = map[string]float64{}
			}
			points[r.Bucket][name] += r.Value
		}
		// 固定图例顺序，没数据的池也保留，避免图例跳来跳去
		keys := []string{"专属", "弹性", "未分配"}
		buckets := make([]int64, 0, len(points))
		for b := range points {
			buckets = append(buckets, b)
		}
		sort.Slice(buckets, func(i, j int) bool { return buckets[i] < buckets[j] })
		out := make([]map[string]any, 0, len(buckets))
		for _, b := range buckets {
			item := map[string]any{"ts": b}
			for _, k := range keys {
				item[k] = points[b][k]
			}
			out = append(out, item)
		}
		return map[string]any{"step": step, "keys": keys, "points": out}, nil

	default: // host
		var hosts []FleetHost
		DB.Find(&hosts)
		nameMap := make(map[int]string, len(hosts))
		for _, h := range hosts {
			nameMap[h.Id] = h.Hostname
		}
		tz := fleetTZOffset()
		DB.Raw(`SELECT ((ts + ?) / ?) * ? - ? AS bucket, host_id AS key, AVG(util) AS value
		        FROM fleet_gpu_samples WHERE ts >= ? AND ts <= ?
		        GROUP BY bucket, host_id ORDER BY bucket`,
			tz, step, step, tz, start, end).Scan(&rows)

		keys := make([]string, 0, len(nameMap))
		for _, name := range nameMap {
			keys = append(keys, name)
		}
		sort.Strings(keys)

		points := map[int64]map[string]float64{}
		for _, r := range rows {
			name, ok := nameMap[r.Key]
			if !ok {
				name = "unknown"
			}
			if points[r.Bucket] == nil {
				points[r.Bucket] = map[string]float64{}
			}
			points[r.Bucket][name] = r.Value
		}
		buckets := make([]int64, 0, len(points))
		for b := range points {
			buckets = append(buckets, b)
		}
		sort.Slice(buckets, func(i, j int) bool { return buckets[i] < buckets[j] })
		out := make([]map[string]any, 0, len(buckets))
		for _, b := range buckets {
			item := map[string]any{"ts": b}
			for name, v := range points[b] {
				item[name] = v
			}
			out = append(out, item)
		}
		return map[string]any{"step": step, "keys": keys, "points": out}, nil
	}
}

type FleetAppView struct {
	Name string `json:"name"`
	// DisplayName：别名解析后的业务名（没有人标记时等于 name）
	DisplayName string `json:"display_name"`
	// Detail：这个应用在服务什么（模型名 / 服务名）
	Detail   string  `json:"detail"`
	Owner    string  `json:"owner"`
	Kind     string  `json:"kind"`
	Host     string  `json:"host"`
	GpuUtil  float64 `json:"gpu_util"`
	GpuCount int     `json:"gpu_count"`
	GpuHours float64 `json:"gpu_hours"`
	// CoveredHours：该应用在统计窗口内实际被采样的小时数。
	// GPU·h 只按这段时间折算，避免刚启动的应用被按 24 小时放大。
	CoveredHours float64 `json:"covered_hours"`
	VramUsed     int64   `json:"vram_used"`
	Estimated    bool    `json:"estimated"`
}

// ListFleetApps 近 24 小时按应用聚合，返回排行。
// ListFleetApps 按「时间区间 + 机器」筛选后的应用排行。
// rangeDays：统计窗口天数；hostId：0 表示全部机器。
func ListFleetApps(topN int, rangeDays int, hostId int) ([]FleetAppView, error) {
	if topN <= 0 {
		topN = 10
	}
	if rangeDays <= 0 {
		rangeDays = 1
	}

	var apps []FleetApp
	appQuery := DB.Model(&FleetApp{})
	if hostId > 0 {
		appQuery = appQuery.Where("host_id = ?", hostId)
	}
	// 只保留“当前还在跑”的应用：最近 10 分钟内有上报。
	// 否则容器删了/服务停了，旧记录会一直挂在排行里显示成僵数据。
	appQuery = appQuery.Where("last_ts >= ?", time.Now().Unix()-600)
	// 纯 CPU 的应用（既不占卡也不占显存）不该出现在算力排行里
	appQuery = appQuery.Where("gpu_count > 0 OR vram_used > 0")
	if err := appQuery.Find(&apps).Error; err != nil {
		return nil, err
	}
	AttachAppAliases(apps)

	var hosts []FleetHost
	DB.Find(&hosts)
	hostName := make(map[int]string, len(hosts))
	for _, h := range hosts {
		hostName[h.Id] = h.Hostname
	}

	type agg struct {
		AppId   int
		Util    float64
		MinTs   int64
		MaxTs   int64
		Samples int64
	}
	var rows []agg
	// 窗口：1 天从当天 0 点算起，多天从 now-N 天算起（与顶部统计卡一致）
	var since int64
	if rangeDays == 1 {
		since = fleetDayStart()
	} else {
		since = time.Now().Unix() - int64(rangeDays)*86400
	}

	if len(apps) == 0 {
		return []FleetAppView{}, nil
	}
	ids := make([]int, 0, len(apps))
	for _, a := range apps {
		ids = append(ids, a.Id)
	}
	DB.Model(&FleetAppSample{}).
		Select(`app_id,
		        AVG(gpu_util) as util,
		        MIN(ts) as min_ts,
		        MAX(ts) as max_ts,
		        COUNT(DISTINCT ts) as samples`).
		Where("ts >= ? AND app_id IN ?", since, ids).
		Group("app_id").Scan(&rows)

	type stat struct {
		util    float64
		gpuHour float64
		covered float64
	}
	statMap := make(map[int]stat, len(rows))
	for _, r := range rows {
		// GPU·h 只能按“实际被采样的时间跨度”折算。
		// 用固定窗口长度会把今天刚启动的应用放大十几倍。
		covered := 0.0
		if r.Samples > 1 && r.MaxTs > r.MinTs {
			covered = float64(r.MaxTs-r.MinTs) / 3600
			windowHours := float64(rangeDays) * 24
			if covered > windowHours {
				covered = windowHours
			}
		}
		statMap[r.AppId] = stat{
			util:    r.Util,
			gpuHour: r.Util / 100 * covered,
			covered: covered,
		}
	}

	views := make([]FleetAppView, 0, len(apps))
	for _, a := range apps {
		st := statMap[a.Id]
		util := st.util
		if util == 0 {
			util = a.GpuUtil
		}
		views = append(views, FleetAppView{
			Name: a.Name, DisplayName: a.DisplayName, Detail: a.Detail, Owner: a.OwnerName,
			Kind: a.Kind, Host: hostName[a.HostId],
			GpuUtil: util, GpuCount: a.GpuCount, GpuHours: st.gpuHour, CoveredHours: st.covered,
			VramUsed: a.VramUsed, Estimated: a.Estimated,
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].GpuUtil > views[j].GpuUtil })
	if len(views) > topN {
		views = views[:topN]
	}
	return views, nil
}

// ===========================================================================
// 人工台账
// ===========================================================================

func GetFleetMeta(hostId int) (*FleetHostMeta, error) {
	var meta FleetHostMeta
	err := DB.Where("host_id = ?", hostId).First(&meta).Error
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

type FleetMetaPatch struct {
	DisplayName   *string `json:"display_name"`
	Room          *string `json:"room"`
	Rack          *string `json:"rack"`
	DeviceModel   *string `json:"device_model"`
	FormFactor    *string `json:"form_factor"`
	AssetNo       *string `json:"asset_no"`
	SerialNo      *string `json:"serial_no"`
	BmcIp         *string `json:"bmc_ip"`
	Owner         *string `json:"owner"`
	Usage         *string `json:"usage"`
	Env           *string `json:"env"`
	PurchasedAt   *string `json:"purchased_at"`
	WarrantyUntil *string `json:"warranty_until"`
}

// UpsertFleetMeta 写入台账，返回字段级 diff（用于历史与前端提示）。
func UpsertFleetMeta(hostId int, patch FleetMetaPatch, by, source string) (map[string]any, error) {
	var current FleetHostMeta
	err := DB.Where("host_id = ?", hostId).First(&current).Error
	if err != nil {
		current = FleetHostMeta{HostId: hostId}
	}

	before := metaToMap(current)
	applyMetaPatch(&current, patch)
	current.UpdatedAt = time.Now().Unix()
	current.UpdatedBy = by

	if err := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&current).Error; err != nil {
		return nil, err
	}
	after := metaToMap(current)

	diff := map[string]any{}
	for k, v := range after {
		if before[k] != v {
			diff[k] = map[string]any{"old": before[k], "new": v}
		}
	}
	if len(diff) > 0 {
		diffBytes, _ := json.Marshal(diff)
		snapBytes, _ := json.Marshal(after)
		DB.Create(&FleetMetaHistory{
			HostId: hostId, ChangedAt: current.UpdatedAt, ChangedBy: by,
			Source: source, DiffJson: string(diffBytes), SnapshotJson: string(snapBytes),
		})
	}
	return diff, nil
}

func applyMetaPatch(m *FleetHostMeta, p FleetMetaPatch) {
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	set(&m.DisplayName, p.DisplayName)
	set(&m.Room, p.Room)
	set(&m.Rack, p.Rack)
	set(&m.DeviceModel, p.DeviceModel)
	set(&m.FormFactor, p.FormFactor)
	set(&m.AssetNo, p.AssetNo)
	set(&m.SerialNo, p.SerialNo)
	set(&m.BmcIp, p.BmcIp)
	set(&m.Owner, p.Owner)
	set(&m.Usage, p.Usage)
	set(&m.Env, p.Env)
	set(&m.PurchasedAt, p.PurchasedAt)
	set(&m.WarrantyUntil, p.WarrantyUntil)
}

func metaToMap(m FleetHostMeta) map[string]string {
	return map[string]string{
		"display_name": m.DisplayName, "room": m.Room, "rack": m.Rack,
		"device_model": m.DeviceModel, "form_factor": m.FormFactor,
		"asset_no": m.AssetNo, "serial_no": m.SerialNo, "bmc_ip": m.BmcIp,
		"owner": m.Owner, "usage": m.Usage, "env": m.Env,
		"purchased_at": m.PurchasedAt, "warranty_until": m.WarrantyUntil,
	}
}

func ListFleetMetaHistory(hostId int, limit int) ([]FleetMetaHistory, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []FleetMetaHistory
	err := DB.Where("host_id = ?", hostId).
		Order("changed_at DESC, id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func RollbackFleetMeta(hostId int, historyId int, by string) error {
	var row FleetMetaHistory
	if err := DB.Where("id = ? AND host_id = ?", historyId, hostId).First(&row).Error; err != nil {
		return err
	}
	var snapshot map[string]string
	if err := json.Unmarshal([]byte(row.SnapshotJson), &snapshot); err != nil {
		return err
	}
	meta := FleetHostMeta{
		HostId: hostId, DisplayName: snapshot["display_name"], Room: snapshot["room"],
		Rack: snapshot["rack"], DeviceModel: snapshot["device_model"],
		FormFactor: snapshot["form_factor"], AssetNo: snapshot["asset_no"],
		SerialNo: snapshot["serial_no"], BmcIp: snapshot["bmc_ip"],
		Owner: snapshot["owner"], Usage: snapshot["usage"], Env: snapshot["env"],
		PurchasedAt: snapshot["purchased_at"], WarrantyUntil: snapshot["warranty_until"],
		UpdatedAt: time.Now().Unix(), UpdatedBy: by,
	}
	if err := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&meta).Error; err != nil {
		return err
	}
	diffBytes, _ := json.Marshal(map[string]any{"rollback_to": row.Id})
	snapBytes, _ := json.Marshal(metaToMap(meta))
	DB.Create(&FleetMetaHistory{
		HostId: hostId, ChangedAt: meta.UpdatedAt, ChangedBy: by,
		Source: "rollback", DiffJson: string(diffBytes), SnapshotJson: string(snapBytes),
	})
	return nil
}

// ImportFleetMeta 批量导入台账：按 ip / display_name / hostname 匹配已有机器。
type FleetMetaImportItem struct {
	Ip          string `json:"ip"`
	Hostname    string `json:"hostname"`
	MachineUuid string `json:"machine_uuid"`
	FleetMetaPatch
}

type FleetMetaImportResult struct {
	DryRun    bool             `json:"dry_run"`
	Matched   []map[string]any `json:"matched"`
	Unmatched []string         `json:"unmatched"`
	Applied   int              `json:"applied"`
}

func ImportFleetMeta(items []FleetMetaImportItem, by string, dryRun bool) (*FleetMetaImportResult, error) {
	result := &FleetMetaImportResult{DryRun: dryRun, Matched: []map[string]any{}, Unmatched: []string{}}

	var hosts []FleetHost
	if err := DB.Find(&hosts).Error; err != nil {
		return nil, err
	}
	byUuid := make(map[string]FleetHost, len(hosts))
	byIp := make(map[string]FleetHost, len(hosts))
	byName := make(map[string]FleetHost, len(hosts))
	for _, h := range hosts {
		if h.MachineUuid != "" {
			byUuid[h.MachineUuid] = h
		}
		if h.Ip != "" {
			byIp[h.Ip] = h
		}
		if h.Hostname != "" {
			byName[h.Hostname] = h
		}
	}

	// 匹配优先级：机器 UUID > hostname > IP。IP 放最后，因为它会被重新分配。
	for _, item := range items {
		var (
			host FleetHost
			ok   bool
			key  string
		)
		if item.MachineUuid != "" {
			if host, ok = byUuid[item.MachineUuid]; ok {
				key = "uuid:" + item.MachineUuid
			}
		}
		if !ok && item.Hostname != "" {
			if host, ok = byName[item.Hostname]; ok {
				key = "hostname:" + item.Hostname
			}
		}
		if !ok && item.Ip != "" {
			if host, ok = byIp[item.Ip]; ok {
				key = "ip:" + item.Ip
			}
		}
		if !ok {
			result.Unmatched = append(result.Unmatched,
				strings.TrimSpace(item.Ip+" "+item.Hostname+" "+item.MachineUuid))
			continue
		}
		if !dryRun {
			if _, err := UpsertFleetMeta(host.Id, item.FleetMetaPatch, by, "import"); err != nil {
				return nil, err
			}
			result.Applied++
		}
		result.Matched = append(result.Matched, map[string]any{
			"host_id": host.Id, "hostname": host.Hostname, "ip": host.Ip, "matched_by": key,
		})
	}
	return result, nil
}

// 让 Time 包被引用（保留便于后续扩展时间字段）
var _ = time.Now
var _ = gorm.ErrRecordNotFound
