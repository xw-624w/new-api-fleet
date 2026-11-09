package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// fleet-agent: compute fleet collector.
// Deps: system tools only (nvidia-smi / docker / /proc). No third-party libs.

type gpuReport struct {
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

type appReport struct {
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Image          string `json:"image"`
	ContainerId    string `json:"container_id"`
	ComposeService string `json:"compose_service"`
	// 以下是“这个应用在服务什么”的线索
	Display   string  `json:"display,omitempty"` // 部署时用 label fleet.name 声明的业务名
	Owner     string  `json:"owner,omitempty"`   // label fleet.owner
	Detail    string  `json:"detail,omitempty"`  // 探测到的模型名 / 服务名
	GpuUtil   float64 `json:"gpu_util"`
	GpuCount  int     `json:"gpu_count"` // 物理占用卡数（在哪几张卡上有显存分配）
	VramUsed  int64   `json:"vram_used"`
	Estimated bool    `json:"estimated"`
}

type hostMetrics struct {
	Cpu     float64 `json:"cpu"`
	Load1   float64 `json:"load1"`
	MemUsed int64   `json:"mem_used"`
	NetRx   float64 `json:"net_rx"`
	NetTx   float64 `json:"net_tx"`
}

type report struct {
	Hostname    string      `json:"hostname"`
	Ip          string      `json:"ip"`
	Os          string      `json:"os"`
	CpuModel    string      `json:"cpu_model"`
	CpuCores    int         `json:"cpu_cores"`
	MemTotal    int64       `json:"mem_total"`
	MachineUuid string      `json:"machine_uuid"`
	Ts          int64       `json:"ts"`
	Gpus        []gpuReport `json:"gpus"`
	Apps        []appReport `json:"apps"`
	Metrics     hostMetrics `json:"host_metrics"`
}

type containerInfo struct {
	ID      string
	Name    string
	Image   string
	Service string
}

var (
	serverURL = flag.String("server", "http://127.0.0.1:8951", "new-api base url")
	token     = flag.String("token", "", "agent token")
	interval  = flag.Duration("interval", 30*time.Second, "collect interval")
	once      = flag.Bool("once", false, "collect and push once, then exit")
	simulate  = flag.String("simulate", "", "simulate mode: path to demo inventory json")
	hostname  = flag.String("hostname", "", "override hostname")
	dryRun    = flag.Bool("dry-run", false, "print payload instead of pushing")
	verbose   = flag.Bool("v", false, "verbose logging")
	// 采集端自更新：服务端换代后自动跟上，不用逐台登录
	autoUpdate = flag.Bool("auto-update", true, "auto-install newer agent from server")
)

func main() {
	flag.Parse()
	if *simulate != "" {
		runSimulate()
		return
	}
	updater := newSelfUpdater()
	for {
		rep, err := collectLocal()
		if err != nil {
			log.Printf("collect failed: %v", err)
		} else if err := push(rep); err != nil {
			log.Printf("push failed: %v", err)
		}
		if *once {
			return
		}
		if updater.maybeUpdate() {
			restartSelf()
		}
		time.Sleep(*interval)
	}
}

func push(rep *report) error {
	body, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Println(string(body))
		return nil
	}
	req, err := http.NewRequest(http.MethodPost,
		strings.TrimRight(*serverURL, "/")+"/api/fleet/ingest", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+*token)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d: %s", resp.StatusCode, string(payload))
	}
	// 服务端约定：HTTP 200 + {"success":false,"message":"..."} 表示被拒绝，
	// 例如 token 失效、时间戳漂移。不检查这里会导致静默丢数据。
	var env struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &env); err == nil && !env.Success && env.Message != "" {
		return fmt.Errorf("server rejected: %s", env.Message)
	}
	if *verbose {
		log.Printf("pushed %s: %s", rep.Hostname, strings.TrimSpace(string(payload)))
	}
	return nil
}

func collectLocal() (*report, error) {
	host, _ := os.Hostname()
	if *hostname != "" {
		host = *hostname
	}
	rep := &report{
		Hostname: host, Ts: time.Now().Unix(), Ip: primaryIP(),
		Os: osPrettyName(), CpuModel: cpuModel(), CpuCores: runtime.NumCPU(),
		MachineUuid: machineUUID(),
	}
	rep.MemTotal, rep.Metrics = readMemory()
	rep.Metrics.Cpu = readCPULoadPercent()
	rep.Metrics.Load1 = readLoad1()
	rep.Metrics.NetRx, rep.Metrics.NetTx = readNetMbps()

	containers := listContainers()
	nvidia := readNvidiaSMI()
	rep.Gpus = append(rep.Gpus, nvidia...)
	// Intel Arc：xpu-smi 优先，没有就退回 sysfs（读数略粗，但绝不自造数据）
	rep.Gpus = append(rep.Gpus, readIntelGPUs()...)
	// 进程级归因只对 NVIDIA 有效（nvidia-smi --query-compute-apps），
	// 混算会把 Arc 的显存算进 NVIDIA 应用的分母里。
	rep.Apps = attributeApps(nvidia, containers)
	return rep, nil
}

// ---------------------------------------------------------------------------
// Intel Arc 采集
// ---------------------------------------------------------------------------

// readIntelGPUs 先试 xpu-smi（Intel XPU Manager），失败再用 sysfs。
// sysfs 拿不到“每卡型号名”和“进程级占用”，只能给出卡级利用率与显存。
func readIntelGPUs() []gpuReport {
	if gpus, ok := readIntelXPUSMI(); ok {
		return gpus
	}
	return readIntelSysfs()
}

func readIntelXPUSMI() ([]gpuReport, bool) {
	if _, err := exec.LookPath("xpu-smi"); err != nil {
		return nil, false
	}
	// xpu-smi discovery -j 列出设备；stats -d N -j 取实时指标
	out, err := exec.Command("xpu-smi", "discovery", "-j").Output()
	if err != nil {
		return nil, false
	}
	ids := extractInts(string(out), `"device_id"\s*:\s*(\d+)`)
	if len(ids) == 0 {
		return nil, false
	}

	var gpus []gpuReport
	for idx, devID := range ids {
		util, memUsed, memTotal, temp, power := intelStatsViaXPUSMI(devID)
		gpus = append(gpus, gpuReport{
			Vendor: "intel", Index: idx,
			Uuid:  fmt.Sprintf("intel-%d", devID),
			Model: intelModelName(), MemTotal: memTotal, MemUsed: memUsed,
			Util: util, Temp: temp, Power: power,
		})
	}
	return gpus, len(gpus) > 0
}

func intelStatsViaXPUSMI(devID int) (util float64, memUsed, memTotal int64, temp, power float64) {
	out, err := exec.Command("xpu-smi", "stats", "-d", strconv.Itoa(devID), "-j").Output()
	if err != nil {
		return 0, 0, 0, 0, 0
	}
	text := string(out)
	util = firstFloat(text, `"(?:gpu_utilization|GPU Utilization \(%\))"\s*:\s*([\d.]+)`)
	memUsed = int64(firstFloat(text, `"(?:used_memory|Memory Used \(MiB\))"\s*:\s*([\d.]+)`))
	memTotal = int64(firstFloat(text, `"(?:total_memory|Memory Total \(MiB\))"\s*:\s*([\d.]+)`))
	temp = firstFloat(text, `"(?:gpu_temperature|GPU Temperature \(°C\))"\s*:\s*([\d.]+)`)
	power = firstFloat(text, `"(?:gpu_power|GPU Power \(W\))"\s*:\s*([\d.]+)`)
	return
}

func readIntelSysfs() []gpuReport {
	root := os.Getenv("FLEET_SYSFS_ROOT")
	entries, err := filepath.Glob(root + "/sys/class/drm/card[0-9]*/device/gpu_busy_percent")
	if err != nil || len(entries) == 0 {
		return nil
	}
	var gpus []gpuReport
	idx := 0
	for _, busyPath := range entries {
		devDir := filepath.Dir(busyPath)
		memTotal := readInt64File(filepath.Join(devDir, "mem_info_vram_total"))
		// 只有独立显卡才有 VRAM；集显跳过，避免把核显混进算力池
		if memTotal <= 0 {
			continue
		}
		memUsed := readInt64File(filepath.Join(devDir, "mem_info_vram_used"))
		util, _ := strconv.ParseFloat(strings.TrimSpace(readTrim(busyPath)), 64)
		temp := intelSysfsTemp(devDir)
		gpus = append(gpus, gpuReport{
			Vendor: "intel", Index: idx,
			Uuid:     fmt.Sprintf("intel-sysfs-%d", idx),
			Model:    intelModelName(),
			MemTotal: memTotal / 1048576, MemUsed: memUsed / 1048576,
			Util: util, Temp: temp,
		})
		idx++
	}
	return gpus
}

func intelModelName() string {
	if fake := os.Getenv("FLEET_LSPCI_FILE"); fake != "" {
		if b, err := os.ReadFile(fake); err == nil {
			return parseLspciIntel(string(b))
		}
	}
	out, err := exec.Command("lspci", "-nn").Output()
	if err != nil {
		return "Intel Arc"
	}
	return parseLspciIntel(string(out))
}

var pciIDPattern = regexp.MustCompile(`\[\w{4}:\w{4}\]`)
var revPattern = regexp.MustCompile(`\s*\(rev [0-9a-fA-F]+\)\s*$`)

func parseLspciIntel(out string) string {
	var fallback string
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "8086:") {
			continue
		}
		if !strings.Contains(line, "VGA") && !strings.Contains(line, "Display") {
			continue
		}
		text := line
		// 去掉 pci id 的方括号（[8086:56a0]），保留型号名里的方括号（DG2 [Arc A770]）
		text = pciIDPattern.ReplaceAllString(text, "")
		if i := strings.Index(text, "Intel Corporation"); i >= 0 {
			text = text[i+len("Intel Corporation"):]
		}
		text = strings.TrimSpace(strings.Trim(text, ":"))
		text = strings.TrimSpace(revPattern.ReplaceAllString(text, ""))
		if text == "" {
			continue
		}
		// 优先认独立卡（Arc / DG2），避免把核显的型号名写到 Arc 上
		if strings.Contains(text, "Arc") || strings.Contains(text, "DG2") {
			return text
		}
		if fallback == "" {
			fallback = text
		}
	}
	if fallback != "" {
		return fallback
	}
	return "Intel Arc"
}

func intelSysfsTemp(devDir string) float64 {
	// hwmonN 编号不固定，按 glob 取第一个温度传感器
	paths, err := filepath.Glob(filepath.Join(devDir, "hwmon", "hwmon*", "temp1_input"))
	if err != nil || len(paths) == 0 {
		return 0
	}
	return float64(readInt64File(paths[0])) / 1000
}

func readInt64File(path string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(readTrim(path)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func firstFloat(text, pattern string) float64 {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

func extractInts(text, pattern string) []int {
	re := regexp.MustCompile(pattern)
	var out []int
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		v, err := strconv.Atoi(m[1])
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

// machineUUID 机器身份：优先 DMI 主板 UUID，取不到再用 /etc/machine-id。
// 换网卡/换 IP/改主机名都不影响它，是台账能长期跟住同一台机器的关键。
func machineUUID() string {
	if v := readTrim("/sys/class/dmi/id/product_uuid"); v != "" && v != "None" {
		return v
	}
	if v := readTrim("/etc/machine-id"); v != "" {
		return v
	}
	if v := readTrim("/var/lib/dbus/machine-id"); v != "" {
		return v
	}
	return ""
}

func primaryIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if v4 := ipnet.IP.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}

func osPrettyName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return runtime.GOOS
}

func cpuModel() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readMemory() (int64, hostMetrics) {
	var m hostMetrics
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, m
	}
	defer f.Close()
	var total, available int64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			available = v
		}
	}
	m.MemUsed = (total - available) / 1024
	return total / 1024, m
}

func readLoad1() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func readCPULoadPercent() float64 {
	idle1, total1 := cpuTimes()
	time.Sleep(200 * time.Millisecond)
	idle2, total2 := cpuTimes()
	dt, di := total2-total1, idle2-idle1
	if dt == 0 {
		return 0
	}
	return float64(dt-di) / float64(dt) * 100
}

func cpuTimes() (idle, total uint64) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}
	line := strings.SplitN(string(b), "\n", 2)[0]
	fields := strings.Fields(line)
	for i, f := range fields {
		if i == 0 {
			continue
		}
		v, _ := strconv.ParseUint(f, 10, 64)
		total += v
		if i == 4 || i == 5 {
			idle += v
		}
	}
	return idle, total
}

func readNetMbps() (rx, tx float64) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	r1, t1 := sumNet(b)
	time.Sleep(200 * time.Millisecond)
	b2, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	r2, t2 := sumNet(b2)
	return float64(r2-r1) / 0.2 / 1024 / 1024, float64(t2-t1) / 0.2 / 1024 / 1024
}

func sumNet(b []byte) (rx, tx uint64) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if strings.TrimSpace(parts[0]) == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		v1, _ := strconv.ParseUint(fields[0], 10, 64)
		v2, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += v1
		tx += v2
	}
	return rx, tx
}

func readNvidiaSMI() []gpuReport {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil
	}
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,uuid,name,memory.total,memory.used,utilization.gpu,temperature.gpu,power.draw,mig.mode.current",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil
	}
	var gpus []gpuReport
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		cells := strings.Split(line, ",")
		if len(cells) < 9 {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		idx, _ := strconv.Atoi(cells[0])
		memTotal, _ := strconv.ParseInt(cells[3], 10, 64)
		memUsed, _ := strconv.ParseInt(cells[4], 10, 64)
		util, _ := strconv.ParseFloat(cells[5], 64)
		temp, _ := strconv.ParseFloat(cells[6], 64)
		power, _ := strconv.ParseFloat(strings.TrimSuffix(cells[7], " W"), 64)
		gpus = append(gpus, gpuReport{
			Vendor: "nvidia", Index: idx, Uuid: cells[1], Model: cells[2],
			MemTotal: memTotal, MemUsed: memUsed, Util: util, Temp: temp,
			Power: power, MigMode: cells[8],
		})
	}
	return gpus
}

func listContainers() []containerInfo {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}
	format := `{{.ID}}|{{.Names}}|{{.Image}}|{{.Label "com.docker.compose.service"}}`
	out, err := exec.Command("docker", "ps", "--format", format).Output()
	if err != nil {
		return nil
	}
	var list []containerInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cells := strings.Split(line, "|")
		for len(cells) < 4 {
			cells = append(cells, "")
		}
		list = append(list, containerInfo{
			ID: strings.TrimSpace(cells[0]), Name: strings.TrimSpace(cells[1]),
			Image: strings.TrimSpace(cells[2]), Service: strings.TrimSpace(cells[3]),
		})
	}
	return list
}

// 应用归因见 attribution.go

// ---------------------------------------------------------------------------
// Simulate mode: drive the whole pipeline with a fixed inventory.
// ---------------------------------------------------------------------------

type simGpu struct {
	Vendor   string  `json:"vendor"`
	Model    string  `json:"model"`
	Count    int     `json:"count"`
	BaseUtil float64 `json:"base_util"`
	MemTotal int64   `json:"mem_total"`
}

type simApp struct {
	Name   string  `json:"name"`
	Kind   string  `json:"kind"`
	Util   float64 `json:"util"`
	Vram   int64   `json:"vram"`
	HasGpu bool    `json:"has_gpu"`
	Gpus   int     `json:"gpus"` // 模拟用：物理占用卡数
}

type simHost struct {
	Hostname string   `json:"hostname"`
	Ip       string   `json:"ip"`
	MemTotal int64    `json:"mem_total"`
	Cpu      float64  `json:"cpu"`
	Gpus     []simGpu `json:"gpus"`
	Apps     []simApp `json:"apps"`
}

func runSimulate() {
	raw, err := os.ReadFile(*simulate)
	if err != nil {
		log.Fatalf("read simulate file: %v", err)
	}
	var hosts []simHost
	if err := json.Unmarshal(raw, &hosts); err != nil {
		log.Fatalf("parse simulate file: %v", err)
	}
	log.Printf("simulating %d hosts against %s", len(hosts), *serverURL)

	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	for {
		for _, h := range hosts {
			if err := push(buildSimReport(h, rnd)); err != nil {
				log.Printf("push %s failed: %v", h.Hostname, err)
			}
		}
		if *once {
			return
		}
		time.Sleep(*interval)
	}
}

func vendorOf(g simGpu) string {
	if g.Vendor == "" {
		return "nvidia"
	}
	return g.Vendor
}

func buildSimReport(h simHost, rnd *rand.Rand) *report {
	hour := float64(time.Now().Hour()) + float64(time.Now().Minute())/60
	diurnal := 0.35 + 0.65*math.Exp(-(hour-14)*(hour-14)/(2*4.2*4.2))

	rep := &report{
		Hostname: h.Hostname, Ip: h.Ip, Ts: time.Now().Unix(),
		Os: "Ubuntu Server 24.04", CpuCores: 32, MemTotal: h.MemTotal,
	}
	rep.Metrics.Cpu = h.Cpu * diurnal
	rep.Metrics.Load1 = h.Cpu * diurnal / 100 * 32
	rep.Metrics.MemUsed = int64(float64(h.MemTotal) * 0.62)

	idx := 0
	for _, g := range h.Gpus {
		for i := 0; i < g.Count; i++ {
			util := g.BaseUtil * diurnal * (0.85 + rnd.Float64()*0.3)
			if util > 99 {
				util = 99
			}
			memUsed := int64(float64(g.MemTotal) * (0.5 + rnd.Float64()*0.4))
			if g.BaseUtil < 5 {
				util = rnd.Float64() * 2
				memUsed = int64(float64(g.MemTotal) * 0.01)
			}
			rep.Gpus = append(rep.Gpus, gpuReport{
				Vendor: vendorOf(g),
				Index:  idx,
				Uuid:   fmt.Sprintf("%s-gpu-%d", h.Hostname, idx),
				Model:  g.Model, MemTotal: g.MemTotal,
				Util: util, MemUsed: memUsed,
				Temp: 38 + util*0.42, Power: 45 + util*3.6,
			})
			idx++
		}
	}

	for _, a := range h.Apps {
		util := a.Util * diurnal * (0.9 + rnd.Float64()*0.2)
		gpus := a.Gpus
		if gpus <= 0 {
			gpus = 1
		}
		rep.Apps = append(rep.Apps, appReport{
			Name: a.Name, Kind: a.Kind, GpuUtil: util, GpuCount: gpus,
			VramUsed: a.Vram, Estimated: !a.HasGpu,
		})
	}
	return rep
}
