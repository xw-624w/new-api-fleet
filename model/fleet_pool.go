package model

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// ===========================================================================
// 算力区分：卡级归属（专属 / 弹性 / 未分配）
//
// 设计要点：
//  1. 归属的最小单位是「一张卡」，不是一台机器——同机不同卡可以归不同项目。
//  2. 没有归属记录的卡视为 unassigned，不并进弹性池，避免弹性池数字虚高。
//  3. 归属是人工决策，采集端不参与；每次修改写一条历史，可回滚。
//  4. 专属卡的「预留卡时」按卡数×时长算，「有效卡时」按利用率折算。
// ===========================================================================

const (
	FleetPoolDedicated  = "dedicated"
	FleetPoolShared     = "shared"
	FleetPoolUnassigned = "unassigned"

	// 在用判定阈值，与 GetFleetSummary 保持一致，避免同一张卡两处口径不同
	fleetPoolInUseUtil = 5.0
)

// FleetGpuPool 一张卡的归属。
type FleetGpuPool struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	HostId    int    `json:"host_id" gorm:"uniqueIndex:uk_fleet_gpu_pool,priority:1"`
	GpuIndex  int    `json:"gpu_index" gorm:"uniqueIndex:uk_fleet_gpu_pool,priority:2"`
	GpuUuid   string `json:"gpu_uuid" gorm:"type:varchar(128)"`
	Pool      string `json:"pool" gorm:"type:varchar(16);default:'unassigned';index"`
	Project   string `json:"project" gorm:"type:varchar(64);index"`
	Team      string `json:"team" gorm:"type:varchar(64)"`
	Note      string `json:"note" gorm:"type:text"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint;default:0"`
	UpdatedBy string `json:"updated_by" gorm:"type:varchar(64)"`
}

func (FleetGpuPool) TableName() string { return "fleet_gpu_pools" }

// FleetGpuPoolHistory 归属变更历史（diff + 快照，可回滚）。
type FleetGpuPoolHistory struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	HostId       int    `json:"host_id" gorm:"index"`
	GpuIndex     int    `json:"gpu_index"`
	ChangedAt    int64  `json:"changed_at" gorm:"bigint;default:0"`
	ChangedBy    string `json:"changed_by" gorm:"type:varchar(64)"`
	DiffJson     string `json:"diff_json" gorm:"type:text"`
	SnapshotJson string `json:"snapshot_json" gorm:"type:text"`
}

func (FleetGpuPoolHistory) TableName() string { return "fleet_gpu_pool_history" }

func loadFleetPoolMap() map[[2]int]FleetGpuPool {
	var rows []FleetGpuPool
	DB.Find(&rows)
	m := make(map[[2]int]FleetGpuPool, len(rows))
	for _, r := range rows {
		if r.Pool == "" {
			r.Pool = FleetPoolUnassigned
		}
		m[[2]int{r.HostId, r.GpuIndex}] = r
	}
	return m
}

func fleetPoolOf(m map[[2]int]FleetGpuPool, hostId, gpuIndex int) FleetGpuPool {
	if p, ok := m[[2]int{hostId, gpuIndex}]; ok {
		return p
	}
	return FleetGpuPool{HostId: hostId, GpuIndex: gpuIndex, Pool: FleetPoolUnassigned}
}

func poolToMap(p FleetGpuPool) map[string]string {
	return map[string]string{
		"pool": p.Pool, "project": p.Project, "team": p.Team, "note": p.Note,
	}
}

// FleetGpuCard 卡列表视图（归属 + 当前状态 + 该机应用名）。
type FleetGpuCard struct {
	HostId    int      `json:"host_id"`
	Hostname  string   `json:"hostname"`
	GpuIndex  int      `json:"gpu_index"`
	Vendor    string   `json:"vendor"`
	Model     string   `json:"model"`
	MemTotal  int64    `json:"mem_total"`
	Util      float64  `json:"util"`
	MemUsed   int64    `json:"mem_used"`
	Temp      float64  `json:"temp"`
	Pool      string   `json:"pool"`
	Project   string   `json:"project"`
	Team      string   `json:"team"`
	Note      string   `json:"note"`
	UpdatedAt int64    `json:"updated_at"`
	UpdatedBy string   `json:"updated_by"`
	HostApps  []string `json:"host_apps"`
	// Conflict：卡归给 A 项目，但这台机器上跑的应用归属是 B（提示人工复核）
	Conflict bool `json:"conflict"`
}

// ListFleetPoolCards 卡列表，可按池 / 机器 / 项目过滤。
func ListFleetPoolCards(pool string, hostId int, project string) ([]FleetGpuCard, error) {
	poolMap := loadFleetPoolMap()

	var gpus []FleetGpu
	q := DB.Order("host_id ASC, vendor ASC, gpu_index ASC")
	if hostId > 0 {
		q = q.Where("host_id = ?", hostId)
	}
	if err := q.Find(&gpus).Error; err != nil {
		return nil, err
	}

	var hosts []FleetHost
	DB.Find(&hosts)
	hostName := make(map[int]string, len(hosts))
	for _, h := range hosts {
		hostName[h.Id] = h.Hostname
	}

	var apps []FleetApp
	DB.Find(&apps)
	AttachAppAliases(apps)
	hostApps := map[int][]string{}
	hostOwner := map[int]string{}
	for _, a := range apps {
		name := a.DisplayName
		if name == "" {
			name = a.Name
		}
		hostApps[a.HostId] = append(hostApps[a.HostId], name)
		if hostOwner[a.HostId] == "" && a.OwnerName != "" {
			hostOwner[a.HostId] = a.OwnerName
		}
	}

	out := make([]FleetGpuCard, 0, len(gpus))
	for _, g := range gpus {
		p := fleetPoolOf(poolMap, g.HostId, g.GpuIndex)
		if pool != "" && pool != "all" && p.Pool != pool {
			continue
		}
		if project != "" && p.Project != project {
			continue
		}
		conflict := p.Pool == FleetPoolDedicated && p.Project != "" &&
			hostOwner[g.HostId] != "" && hostOwner[g.HostId] != p.Project
		names := hostApps[g.HostId]
		if names == nil {
			names = []string{}
		}
		out = append(out, FleetGpuCard{
			HostId: g.HostId, Hostname: hostName[g.HostId], GpuIndex: g.GpuIndex,
			Vendor: g.Vendor, Model: g.Model, MemTotal: g.MemTotal,
			Util: g.Util, MemUsed: g.MemUsed, Temp: g.Temp,
			Pool: p.Pool, Project: p.Project, Team: p.Team, Note: p.Note,
			UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy,
			HostApps: names, Conflict: conflict,
		})
	}
	return out, nil
}

// SetFleetGpuPool 设置一张卡的归属，返回字段级 diff。
func SetFleetGpuPool(hostId, gpuIndex int, pool, project, team, note, by string) (map[string]any, error) {
	pool = strings.ToLower(strings.TrimSpace(pool))
	project = strings.TrimSpace(project)
	team = strings.TrimSpace(team)
	note = strings.TrimSpace(note)

	switch pool {
	case FleetPoolDedicated:
		if project == "" {
			return nil, fmt.Errorf("专属卡必须填写项目名")
		}
	case FleetPoolShared:
		project, team = "", ""
	case FleetPoolUnassigned:
		project, team, note = "", "", ""
	default:
		return nil, fmt.Errorf("pool 只能是 dedicated / shared / unassigned")
	}

	var card FleetGpu
	if err := DB.Where("host_id = ? AND gpu_index = ?", hostId, gpuIndex).First(&card).Error; err != nil {
		return nil, fmt.Errorf("这台机器上没有第 %d 张卡", gpuIndex)
	}

	var cur FleetGpuPool
	if err := DB.Where("host_id = ? AND gpu_index = ?", hostId, gpuIndex).First(&cur).Error; err != nil {
		cur = FleetGpuPool{HostId: hostId, GpuIndex: gpuIndex, Pool: FleetPoolUnassigned}
	}
	before := poolToMap(cur)

	cur.GpuUuid = card.Uuid
	cur.Pool, cur.Project, cur.Team, cur.Note = pool, project, team, note
	cur.UpdatedAt = time.Now().Unix()
	cur.UpdatedBy = by

	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "host_id"}, {Name: "gpu_index"}},
		UpdateAll: true,
	}).Create(&cur).Error; err != nil {
		return nil, err
	}

	after := poolToMap(cur)
	diff := map[string]any{}
	for k, v := range after {
		if before[k] != v {
			diff[k] = map[string]any{"old": before[k], "new": v}
		}
	}
	if len(diff) > 0 {
		diffBytes, _ := json.Marshal(diff)
		snapBytes, _ := json.Marshal(after)
		DB.Create(&FleetGpuPoolHistory{
			HostId: hostId, GpuIndex: gpuIndex, ChangedAt: cur.UpdatedAt,
			ChangedBy: by, DiffJson: string(diffBytes), SnapshotJson: string(snapBytes),
		})
	}
	return diff, nil
}

// BatchSetFleetGpuPool 批量打标，返回成功条数。
func BatchSetFleetGpuPool(hostId int, indexes []int, pool, project, team, note, by string) (int, error) {
	if len(indexes) == 0 {
		return 0, fmt.Errorf("没有选择显卡")
	}
	n := 0
	for _, idx := range indexes {
		if _, err := SetFleetGpuPool(hostId, idx, pool, project, team, note, by); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ListFleetGpuPoolHistory 某张卡的归属变更历史。
func ListFleetGpuPoolHistory(hostId, gpuIndex, limit int) ([]FleetGpuPoolHistory, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []FleetGpuPoolHistory
	err := DB.Where("host_id = ? AND gpu_index = ?", hostId, gpuIndex).
		Order("changed_at DESC, id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// RollbackFleetGpuPool 回滚到某条历史快照。
func RollbackFleetGpuPool(hostId, gpuIndex, historyId int, by string) error {
	var row FleetGpuPoolHistory
	if err := DB.Where("id = ? AND host_id = ? AND gpu_index = ?", historyId, hostId, gpuIndex).
		First(&row).Error; err != nil {
		return err
	}
	var snap map[string]string
	if err := json.Unmarshal([]byte(row.SnapshotJson), &snap); err != nil {
		return err
	}
	_, err := SetFleetGpuPool(hostId, gpuIndex, snap["pool"], snap["project"], snap["team"], snap["note"], by)
	return err
}

// ListFleetProjects 项目下拉：归属表里出现过的项目 + 应用别名里的 owner。
func ListFleetProjects() ([]string, error) {
	set := map[string]struct{}{}
	var pools []FleetGpuPool
	DB.Where("project <> ''").Find(&pools)
	for _, p := range pools {
		set[p.Project] = struct{}{}
	}
	var aliases []FleetAppAlias
	DB.Find(&aliases)
	for _, a := range aliases {
		if v := strings.TrimSpace(a.Owner); v != "" {
			set[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// --- 分池统计 ---------------------------------------------------------------

type FleetPoolStat struct {
	Pool      string  `json:"pool"`
	Cards     int     `json:"cards"`
	InUse     int     `json:"in_use"`
	Idle      int     `json:"idle"`
	VramUsed  int64   `json:"vram_used"`
	VramTotal int64   `json:"vram_total"`
	AvgUtil   float64 `json:"avg_util"`
	GpuHours  float64 `json:"gpu_hours"`
	Projects  int     `json:"projects"`
}

type FleetPoolProject struct {
	Pool     string  `json:"pool"`
	Project  string  `json:"project"`
	Cards    int     `json:"cards"`
	GpuHours float64 `json:"gpu_hours"`
	AvgUtil  float64 `json:"avg_util"`
}

func fleetPoolWindowStart(rangeDays int) int64 {
	if rangeDays <= 0 {
		rangeDays = 1
	}
	if rangeDays == 1 {
		return fleetDayStart()
	}
	return time.Now().Unix() - int64(rangeDays)*86400
}

// GetFleetPoolStats 分池当前值 + 区间卡时。
func GetFleetPoolStats(rangeDays int) (map[string]*FleetPoolStat, []FleetPoolProject, error) {
	stats := map[string]*FleetPoolStat{
		FleetPoolDedicated:  {Pool: FleetPoolDedicated},
		FleetPoolShared:     {Pool: FleetPoolShared},
		FleetPoolUnassigned: {Pool: FleetPoolUnassigned},
	}
	get := func(k string) *FleetPoolStat {
		if s, ok := stats[k]; ok {
			return s
		}
		s := &FleetPoolStat{Pool: k}
		stats[k] = s
		return s
	}

	var gpus []FleetGpu
	if err := DB.Find(&gpus).Error; err != nil {
		return nil, nil, err
	}
	poolMap := loadFleetPoolMap()
	projectCards := map[string]map[string]int{} // project -> host:idx
	projectSumUtil := map[string]float64{}
	projectSamples := map[string]int{}
	for _, g := range gpus {
		p := fleetPoolOf(poolMap, g.HostId, g.GpuIndex)
		st := get(p.Pool)
		st.Cards++
		st.VramUsed += g.MemUsed
		st.VramTotal += g.MemTotal
		st.AvgUtil += g.Util
		if g.Util >= fleetPoolInUseUtil {
			st.InUse++
		} else {
			st.Idle++
		}
		if p.Pool == FleetPoolDedicated && p.Project != "" {
			if projectCards[p.Project] == nil {
				projectCards[p.Project] = map[string]int{}
			}
			projectCards[p.Project][fmt.Sprintf("%d:%d", g.HostId, g.GpuIndex)]++
			projectSumUtil[p.Project] += g.Util
			projectSamples[p.Project]++
		}
	}
	for _, st := range stats {
		if st.Cards > 0 {
			st.AvgUtil = st.AvgUtil / float64(st.Cards)
		}
	}
	stats[FleetPoolDedicated].Projects = len(projectCards)

	// 区间卡时：SUM(util)/100 × 采样间隔 / 3600
	var agg []struct {
		Pool     string  `gorm:"column:pool"`
		SumUtil  float64 `gorm:"column:sum_util"`
		Samples  int64   `gorm:"column:samples"`
		Distinct int64   `gorm:"column:distinct"`
		MinTs    int64   `gorm:"column:min_ts"`
		MaxTs    int64   `gorm:"column:max_ts"`
	}
	windowStart := fleetPoolWindowStart(rangeDays)
	DB.Raw(`SELECT COALESCE(p.pool, 'unassigned') AS pool,
	               COALESCE(SUM(s.util), 0) AS sum_util,
	               COUNT(*) AS samples,
	               COUNT(DISTINCT s.ts) AS distinct,
	               MIN(s.ts) AS min_ts, MAX(s.ts) AS max_ts
	        FROM fleet_gpu_samples s
	        LEFT JOIN fleet_gpu_pools p
	               ON p.host_id = s.host_id AND p.gpu_index = s.gpu_index
	        WHERE s.ts >= ?
	        GROUP BY 1`, windowStart).Scan(&agg)

	for _, a := range agg {
		interval := 30.0
		if a.MaxTs > a.MinTs && a.Distinct > 1 {
			interval = float64(a.MaxTs-a.MinTs) / float64(a.Distinct-1)
		}
		if a.Samples > 0 {
			get(a.Pool).GpuHours = a.SumUtil / 100 * interval / 3600
		}
	}

	// 专属池按项目拆
	type prow struct {
		Project  string  `gorm:"column:project"`
		SumUtil  float64 `gorm:"column:sum_util"`
		Samples  int64   `gorm:"column:samples"`
		Distinct int64   `gorm:"column:distinct"`
		MinTs    int64   `gorm:"column:min_ts"`
		MaxTs    int64   `gorm:"column:max_ts"`
	}
	var prows []prow
	DB.Raw(`SELECT p.project AS project,
	               COALESCE(SUM(s.util), 0) AS sum_util,
	               COUNT(*) AS samples,
	               COUNT(DISTINCT s.ts) AS distinct,
	               MIN(s.ts) AS min_ts, MAX(s.ts) AS max_ts
	        FROM fleet_gpu_samples s
	        JOIN fleet_gpu_pools p
	          ON p.host_id = s.host_id AND p.gpu_index = s.gpu_index
	        WHERE s.ts >= ? AND p.pool = ?
	        GROUP BY 1`, windowStart, FleetPoolDedicated).Scan(&prows)

	projects := make([]FleetPoolProject, 0, len(projectCards))
	for name, cards := range projectCards {
		row := FleetPoolProject{Pool: FleetPoolDedicated, Project: name, Cards: len(cards)}
		if projectSamples[name] > 0 {
			row.AvgUtil = projectSumUtil[name] / float64(projectSamples[name])
		}
		for _, pr := range prows {
			if pr.Project != name {
				continue
			}
			interval := 30.0
			if pr.MaxTs > pr.MinTs && pr.Distinct > 1 {
				interval = float64(pr.MaxTs-pr.MinTs) / float64(pr.Distinct-1)
			}
			row.GpuHours = pr.SumUtil / 100 * interval / 3600
		}
		projects = append(projects, row)
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Cards != projects[j].Cards {
			return projects[i].Cards > projects[j].Cards
		}
		return projects[i].GpuHours > projects[j].GpuHours
	})

	return stats, projects, nil
}

// --- 分账 -------------------------------------------------------------------

type FleetBillingRow struct {
	Project        string  `json:"project"`
	DedicatedCards int     `json:"dedicated_cards"`
	ReservedHours  float64 `json:"reserved_hours"`
	EffectiveHours float64 `json:"effective_hours"`
	SharedHours    float64 `json:"shared_hours"`
	TotalHours     float64 `json:"total_hours"`
}

// fleetMonthRange 把 "2026-11" 解析成本地时区的月初/月末时间戳。
func fleetMonthRange(month string) (int64, int64, string, error) {
	now := time.Now()
	var startLocal time.Time
	month = strings.TrimSpace(month)
	if month == "" {
		startLocal = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	} else {
		t, err := time.ParseInLocation("2006-01", month, now.Location())
		if err != nil {
			return 0, 0, "", fmt.Errorf("月份格式应为 YYYY-MM")
		}
		startLocal = t
	}
	endLocal := startLocal.AddDate(0, 1, 0)
	start, end := startLocal.Unix(), endLocal.Unix()
	if end > now.Unix() {
		end = now.Unix()
	}
	return start, end, startLocal.Format("2006-01"), nil
}

// GetFleetBilling 按项目出某个月的用量。
//
//	专属卡：预留卡时 = 卡数 × 区间时长；有效卡时 = 利用率折算
//	弹性卡：按应用分摊，应用→项目取别名的 owner，其次应用自带 owner
func GetFleetBilling(month string) (map[string]any, error) {
	start, end, label, err := fleetMonthRange(month)
	if err != nil {
		return nil, err
	}
	if end <= start {
		return map[string]any{"month": label, "rows": []FleetBillingRow{}}, nil
	}
	periodHours := float64(end-start) / 3600

	rows := map[string]*FleetBillingRow{}
	get := func(project string) *FleetBillingRow {
		if r, ok := rows[project]; ok {
			return r
		}
		r := &FleetBillingRow{Project: project}
		rows[project] = r
		return r
	}

	// 专属卡：预留
	var poolRows []FleetGpuPool
	DB.Where("pool = ?", FleetPoolDedicated).Find(&poolRows)
	for _, p := range poolRows {
		if p.Project == "" {
			continue
		}
		r := get(p.Project)
		r.DedicatedCards++
		r.ReservedHours += periodHours
	}

	// 专属卡：有效（利用率折算）
	var eff []struct {
		Project  string  `gorm:"column:project"`
		SumUtil  float64 `gorm:"column:sum_util"`
		Distinct int64   `gorm:"column:distinct"`
		MinTs    int64   `gorm:"column:min_ts"`
		MaxTs    int64   `gorm:"column:max_ts"`
	}
	DB.Raw(`SELECT p.project AS project, COALESCE(SUM(s.util),0) AS sum_util,
	               COUNT(DISTINCT s.ts) AS distinct, MIN(s.ts) AS min_ts, MAX(s.ts) AS max_ts
	        FROM fleet_gpu_samples s
	        JOIN fleet_gpu_pools p ON p.host_id = s.host_id AND p.gpu_index = s.gpu_index
	        WHERE s.ts >= ? AND s.ts < ? AND p.pool = ?
	        GROUP BY 1`, start, end, FleetPoolDedicated).Scan(&eff)
	for _, e := range eff {
		if e.Project == "" {
			continue
		}
		interval := 30.0
		if e.MaxTs > e.MinTs && e.Distinct > 1 {
			interval = float64(e.MaxTs-e.MinTs) / float64(e.Distinct-1)
		}
		get(e.Project).EffectiveHours = e.SumUtil / 100 * interval / 3600
	}

	// 弹性卡：按应用分摊到项目
	var apps []FleetApp
	DB.Find(&apps)
	AttachAppAliases(apps)
	appProject := make(map[int]string, len(apps))
	appHost := make(map[int]int, len(apps))
	for _, a := range apps {
		project := a.OwnerName
		if project == "" {
			project = a.Owner
		}
		if project == "" {
			project = "未标注应用"
		}
		appProject[a.Id] = project
		appHost[a.Id] = a.HostId
	}

	// 应用采样不知道落在哪张卡上，按「该机非专属卡占比」折算，
	// 避免专属卡上的应用用量被同时算进专属和弹性两边。
	var allGpus []FleetGpu
	DB.Find(&allGpus)
	poolMap := loadFleetPoolMap()
	hostTotal := map[int]int{}
	hostFlexible := map[int]int{}
	for _, g := range allGpus {
		hostTotal[g.HostId]++
		if fleetPoolOf(poolMap, g.HostId, g.GpuIndex).Pool != FleetPoolDedicated {
			hostFlexible[g.HostId]++
		}
	}

	var shared []struct {
		AppId    int     `gorm:"column:app_id"`
		SumUtil  float64 `gorm:"column:sum_util"`
		Distinct int64   `gorm:"column:distinct"`
		MinTs    int64   `gorm:"column:min_ts"`
		MaxTs    int64   `gorm:"column:max_ts"`
	}
	DB.Raw(`SELECT app_id, COALESCE(SUM(gpu_util),0) AS sum_util,
	               COUNT(DISTINCT ts) AS distinct, MIN(ts) AS min_ts, MAX(ts) AS max_ts
	        FROM fleet_app_samples WHERE ts >= ? AND ts < ?
	        GROUP BY app_id`, start, end).Scan(&shared)
	for _, s := range shared {
		project, ok := appProject[s.AppId]
		if !ok {
			project = "未标注应用"
		}
		interval := 30.0
		if s.MaxTs > s.MinTs && s.Distinct > 1 {
			interval = float64(s.MaxTs-s.MinTs) / float64(s.Distinct-1)
		}
		ratio := 1.0
		if h := appHost[s.AppId]; hostTotal[h] > 0 {
			ratio = float64(hostFlexible[h]) / float64(hostTotal[h])
		}
		get(project).SharedHours += s.SumUtil / 100 * interval / 3600 * ratio
	}

	out := make([]FleetBillingRow, 0, len(rows))
	for _, r := range rows {
		r.TotalHours = r.EffectiveHours + r.SharedHours
		if r.TotalHours == 0 && r.ReservedHours > 0 {
			r.TotalHours = r.ReservedHours
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TotalHours > out[j].TotalHours })

	return map[string]any{
		"month":        label,
		"start":        start,
		"end":          end,
		"period_hours": periodHours,
		"rows":         out,
	}, nil
}
