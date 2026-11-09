package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// 应用识别增强（方案 A：自动探测“这个容器在服务什么”）
//
// 优先级：
//   1. 容器 label  fleet.name  （部署时显式声明，最准）
//   2. HTTP 探测 /v1/models（vLLM / OpenAI 兼容）、/api/tags（ollama）
//   3. 启动参数 --served-model-name / --model 的 basename
//   4. 环境变量 MODEL_NAME / MODEL / SERVED_MODEL_NAME
//   5. 空（页面靠人工别名兜底）

type containerDetail struct {
	ID      string
	Name    string
	Image   string
	Service string
	Ports   []int
	Argv    string
	Env     map[string]string
	Labels  map[string]string
}

type dockerInspect struct {
	Config struct {
		Image  string            `json:"Image"`
		Cmd    []string          `json:"Cmd"`
		Entry  []string          `json:"Entrypoint"`
		Env    []string          `json:"Env"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	Name       string `json:"Name"`
	HostConfig struct {
		PortBindings map[string][]struct {
			HostPort string `json:"HostPort"`
		} `json:"PortBindings"`
	} `json:"HostConfig"`
}

func loadContainerDetails(ids []string) map[string]containerDetail {
	out := make(map[string]containerDetail, len(ids))
	if len(ids) == 0 {
		return out
	}
	args := append([]string{"inspect"}, ids...)
	raw, err := exec.Command("docker", args...).Output()
	if err != nil {
		return out
	}

	var items []dockerInspect
	if err := json.Unmarshal(raw, &items); err != nil {
		return out
	}

	for i, it := range items {
		d := containerDetail{
			ID:     ids[i],
			Name:   strings.TrimPrefix(it.Name, "/"),
			Image:  it.Config.Image,
			Labels: it.Config.Labels,
			Env:    parseEnv(it.Config.Env),
			Ports:  extractPorts(it),
		}
		if d.Labels != nil {
			d.Service = d.Labels["com.docker.compose.service"]
		}
		argv := append(append([]string{}, it.Config.Entry...), it.Config.Cmd...)
		d.Argv = strings.Join(argv, " ")
		out[ids[i]] = d
	}
	return out
}

func parseEnv(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

func extractPorts(it dockerInspect) []int {
	var ports []int
	for _, binds := range it.HostConfig.PortBindings {
		for _, b := range binds {
			if p, err := strconv.Atoi(b.HostPort); err == nil && p > 0 {
				ports = append(ports, p)
			}
		}
	}
	return ports
}

var probeClient = &http.Client{Timeout: 1500 * time.Millisecond}

// detectModel 返回容器对外提供的模型名，探测不到就返回空串（绝不自造）。
func detectModel(d containerDetail) string {
	if v := strings.TrimSpace(d.Labels["fleet.model"]); v != "" {
		return v
	}

	lowerImage := strings.ToLower(d.Image)
	lowerName := strings.ToLower(d.Name + " " + d.Service)
	isOllama := strings.Contains(lowerImage, "ollama") || strings.Contains(lowerName, "ollama")

	for _, port := range d.Ports {
		// OpenAI 兼容：vLLM / SGLang / TGI / LMDeploy
		if m := probeOpenAIModels(port); m != "" {
			return m
		}
		if isOllama {
			if m := probeOllamaTags(port); m != "" {
				return m
			}
		}
	}

	if m := modelFromArgv(d.Argv); m != "" {
		return m
	}
	for _, key := range []string{"SERVED_MODEL_NAME", "MODEL_NAME", "MODEL"} {
		if v := strings.TrimSpace(d.Env[key]); v != "" {
			return lastSegment(v)
		}
	}
	return ""
}

func probeOpenAIModels(port int) string {
	resp, err := probeClient.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/v1/models")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return ""
	}
	var payload struct {
		Data []struct {
			Id string `json:"id"`
		} `json:"data"`
		// ollama /api/tags 结构
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if len(payload.Data) > 0 && payload.Data[0].Id != "" {
		return payload.Data[0].Id
	}
	if len(payload.Models) > 0 && payload.Models[0].Name != "" {
		return payload.Models[0].Name
	}
	return ""
}

func probeOllamaTags(port int) string {
	resp, err := probeClient.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/api/tags")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return ""
	}
	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if len(payload.Models) > 0 {
		return payload.Models[0].Name
	}
	return ""
}

func modelFromArgv(argv string) string {
	fields := strings.Fields(argv)
	for i, f := range fields {
		switch {
		case f == "--served-model-name" || f == "-served-model-name":
			if i+1 < len(fields) {
				return fields[i+1]
			}
		case strings.HasPrefix(f, "--served-model-name="):
			return strings.TrimPrefix(f, "--served-model-name=")
		case f == "--model" || f == "--model-path" || f == "-m":
			if i+1 < len(fields) {
				return lastSegment(fields[i+1])
			}
		case strings.HasPrefix(f, "--model="):
			return lastSegment(strings.TrimPrefix(f, "--model="))
		}
	}
	return ""
}

func lastSegment(path string) string {
	path = strings.Trim(path, `"'`)
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 && i < len(path)-1 {
		return path[i+1:]
	}
	return path
}

// enrichApps 给已归因的容器应用补上“在服务什么”。
func enrichApps(apps []appReport, containers []containerInfo) {
	var ids []string
	seen := map[string]bool{}
	for _, a := range apps {
		if a.ContainerId != "" && !seen[a.ContainerId] {
			seen[a.ContainerId] = true
			ids = append(ids, a.ContainerId)
		}
	}
	if len(ids) == 0 {
		return
	}

	details := loadContainerDetails(ids)
	for i := range apps {
		d, ok := details[apps[i].ContainerId]
		if !ok {
			continue
		}
		if v := strings.TrimSpace(d.Labels["fleet.name"]); v != "" {
			apps[i].Display = v
		}
		if v := strings.TrimSpace(d.Labels["fleet.owner"]); v != "" {
			apps[i].Owner = v
		}
		apps[i].Detail = detectModel(d)
		if apps[i].Detail == "" {
			apps[i].Detail = d.Service
		}
	}
}
