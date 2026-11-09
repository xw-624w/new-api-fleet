package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// 应用归因（采集端）
//
// 目标：把“哪张卡上的多少显存/多少利用率，属于哪个容器或哪个宿主机进程”算准。
// 关键点：
//   1. nvidia-smi 要取 gpu_uuid + pid + process_name + used_memory，
//      否则多卡机没法按卡分摊利用率。
//   2. pid → 容器的映射同时兼容：
//        docker( cgroup v1 /v2 、docker-<id>.scope 、/docker/<id> )
//        containerd / k8s( crio ) —— 统一按 cgroup 里的 hex id 前缀匹配。
//   3. 落在宿主机上的 GPU 进程（没进容器）按进程名归类成 host:<name>，不再塞进 unattributed。
//   4. 同一 compose service 在这台机器上有多副本时，名字后面加 #短ID，避免全被合并成一条。

type gpuProc struct {
	GpuUuid  string
	Pid      int
	ProcName string
	Mem      int64
}

var hexIDPattern = regexp.MustCompile(`[0-9a-f]{12,64}`)

func readGpuProcs() []gpuProc {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil
	}
	out, err := exec.Command("nvidia-smi",
		"--query-compute-apps=gpu_uuid,pid,process_name,used_memory",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil
	}

	var procs []gpuProc
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		cells := strings.Split(line, ",")
		if len(cells) < 4 {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		pid, _ := strconv.Atoi(cells[1])
		mem, _ := strconv.ParseInt(cells[3], 10, 64)
		if pid <= 0 {
			continue
		}
		name := strings.Trim(cells[2], `"`)
		if name != "" {
			name = filepath.Base(name)
		}
		procs = append(procs, gpuProc{
			GpuUuid: cells[0], Pid: pid, ProcName: name, Mem: mem,
		})
	}
	return procs
}

func attributeApps(gpus []gpuReport, containers []containerInfo) []appReport {
	if len(gpus) == 0 {
		return nil
	}
	procs := readGpuProcs()
	if len(procs) == 0 {
		return nil
	}

	byUuid := make(map[string]gpuReport, len(gpus))
	for _, g := range gpus {
		byUuid[g.Uuid] = g
	}

	// 同一 service 的副本数：>1 时名字要带短 ID，不然会糊成一条
	serviceCount := make(map[string]int, len(containers))
	for _, c := range containers {
		key := c.Service
		if key == "" {
			key = c.Name
		}
		serviceCount[key]++
	}

	type acc struct {
		app      appReport
		memOnGpu map[string]int64
	}
	apps := make(map[string]*acc)
	procMemOnGpu := make(map[string]int64)
	appsOnGpu := make(map[string]map[string]struct{})

	for _, p := range procs {
		app, ok := resolveApp(p.Pid, p.ProcName, containers, serviceCount)
		if !ok {
			app = appReport{Name: "unknown:" + orDefault(p.ProcName, "pid"), Kind: "unknown"}
		}
		a := apps[app.Name]
		if a == nil {
			a = &acc{app: app, memOnGpu: map[string]int64{}}
			apps[app.Name] = a
		}
		a.memOnGpu[p.GpuUuid] += p.Mem
		a.app.VramUsed += p.Mem
		procMemOnGpu[p.GpuUuid] += p.Mem

		if appsOnGpu[p.GpuUuid] == nil {
			appsOnGpu[p.GpuUuid] = map[string]struct{}{}
		}
		appsOnGpu[p.GpuUuid][app.Name] = struct{}{}
	}

	out := make([]appReport, 0, len(apps))
	for _, a := range apps {
		var util float64
		estimated := false
		for uuid, mem := range a.memOnGpu {
			g, ok := byUuid[uuid]
			if !ok {
				continue
			}
			denom := procMemOnGpu[uuid]
			if denom <= 0 {
				denom = g.MemUsed
			}
			if denom <= 0 {
				continue
			}
			// 同一张卡上跑着多个应用时，利用率只能按显存占比分摊 → 标记估算
			if len(appsOnGpu[uuid]) > 1 {
				estimated = true
			}
			util += g.Util * float64(mem) / float64(denom)
		}
		a.app.GpuUtil = util
		a.app.Estimated = estimated
		// 物理占用：这个应用在哪几张卡上有显存分配
		a.app.GpuCount = len(a.memOnGpu)
		out = append(out, a.app)
	}

	// 给容器类应用补上“在服务什么”（模型名 / 业务名）
	enrichApps(out, containers)

	return out
}

func resolveApp(pid int, procName string, containers []containerInfo, serviceCount map[string]int) (appReport, bool) {
	if cid := containerIDOfPID(pid); cid != "" {
		for _, c := range containers {
			if len(c.ID) < 12 || !strings.HasPrefix(c.ID, cid) {
				continue
			}
			name := c.Service
			if name == "" {
				name = stripReplicaSuffix(c.Name)
			}
			if name == "" {
				name = c.ID[:12]
			}
			if serviceCount[name] > 1 {
				name = fmt.Sprintf("%s#%s", name, c.ID[:4])
			}
			return appReport{
				Name: name, Kind: "container", Image: c.Image,
				ContainerId: c.ID, ComposeService: c.Service,
			}, true
		}
	}

	// 没落在容器里：按进程名归类，别丢进 unattributed
	base := normalizeProcName(procName)
	if base == "" {
		base = normalizeProcName(strings.TrimSpace(readTrim(fmt.Sprintf("/proc/%d/comm", pid))))
	}
	if base == "" {
		return appReport{}, false
	}

	kind := "host"
	cmdline := strings.ToLower(readTrim(fmt.Sprintf("/proc/%d/cmdline", pid)))
	switch {
	case strings.Contains(cmdline, "vllm"):
		kind = "vllm"
	case strings.Contains(cmdline, "ollama"):
		kind = "ollama"
	case strings.Contains(cmdline, "sglang"):
		kind = "sglang"
	}
	return appReport{Name: "host:" + base, Kind: kind}, true
}

// containerIDOfPID 从 /proc/<pid>/cgroup 里提取容器 ID 前缀。
// 同时兼容 docker / containerd / k8s(crio)：它们都会在路径里带 64 位 hex 的容器 ID。
func containerIDOfPID(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return ""
	}
	return parseContainerID(string(b))
}

func parseContainerID(cgroup string) string {
	for _, line := range strings.Split(cgroup, "\n") {
		// cgroup v1: 12:devices:/docker/<id>   或  /system.slice/docker-<id>.scope
		// cgroup v2: 0::/system.slice/docker-<id>.scope
		// containerd/k8s: .../cri-containerd-<id>.scope  或  .../<id>
		if m := hexIDPattern.FindString(line); m != "" {
			if len(m) >= 12 {
				return m[:12]
			}
		}
	}
	return ""
}

func normalizeProcName(name string) string {
	name = strings.TrimSpace(strings.Trim(name, `"`))
	if name == "" {
		return ""
	}
	name = filepath.Base(name)
	// python3.11 → python3，避免同一种进程因为小版本号拆成多个条目
	if strings.HasPrefix(name, "python3.") {
		return "python3"
	}
	if strings.HasSuffix(name, ":") {
		name = strings.TrimSuffix(name, ":")
	}
	return name
}

func stripReplicaSuffix(name string) string {
	// docker compose 的容器名形如 <项目>-<服务>-<序号>，去掉末尾序号
	i := strings.LastIndex(name, "-")
	if i <= 0 {
		return name
	}
	if _, err := strconv.Atoi(name[i+1:]); err == nil {
		return name[:i]
	}
	return name
}

func orDefault(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
