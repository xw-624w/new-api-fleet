package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// 采集端一键安装：由 new-api 自己分发安装脚本和二进制。
// 目标机器上只需要一条 curl | bash，不用传文件、不用手配 systemd。

const agentBinaryPath = "/fleet-agent-bin"

const installScriptTemplate = `#!/bin/bash
# 由 new-api 生成的 fleet-agent 安装脚本
# 机器：__HOST__    服务端：__API__
set -euo pipefail

if [ "$(id -u)" != "0" ]; then
  echo "请用 root 运行：curl -fsSL ... | sudo bash" >&2
  exit 1
fi

API='__API__'
TOKEN='__TOKEN__'
HOST='__HOST__'

echo "== 安装 fleet-agent：$HOST -> $API =="

TMP=$(mktemp)
curl -fsSL "$API/api/fleet/agent/bin?token=$TOKEN" -o "$TMP"
install -m 0755 "$TMP" /usr/local/bin/fleet-agent
rm -f "$TMP"

install -d -m 0750 /etc/fleet-agent
cat > /etc/fleet-agent/fleet-agent.env <<ENVEOF
FLEET_SERVER=$API
FLEET_TOKEN=$TOKEN
FLEET_INTERVAL=30s
ENVEOF
chmod 0600 /etc/fleet-agent/fleet-agent.env

cat > /etc/systemd/system/fleet-agent.service <<'UNITEOF'
[Unit]
Description=compute-fleet agent (New API compute fleet collector)
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/fleet-agent/fleet-agent.env
ExecStart=/usr/local/bin/fleet-agent -server ${FLEET_SERVER} -token ${FLEET_TOKEN} -interval ${FLEET_INTERVAL}
Restart=always
RestartSec=5
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=read-only
PrivateTmp=true

[Install]
WantedBy=multi-user.target
UNITEOF

systemctl daemon-reload
systemctl enable fleet-agent >/dev/null 2>&1
# 关键：服务已在运行时 enable --now 不会重启，必须显式 restart，
# 否则换掉的是磁盘上的二进制，内存里跑的还是旧版本。
systemctl restart fleet-agent
sleep 2
if systemctl is-active --quiet fleet-agent; then
  echo "  服务已重启（启动于 $(systemctl show fleet-agent -p ActiveEnterTimestamp --value)）"
else
  echo "  !! 服务未启动，查看：journalctl -u fleet-agent -n 30 --no-pager" >&2
fi
sleep 3

echo
systemctl --no-pager --lines=3 status fleet-agent || true
echo
echo "== 采集预览（不上报）=="
/usr/local/bin/fleet-agent -once -dry-run 2>/dev/null | head -c 400 || true
echo
echo
echo "完成。打开 $API/dashboard/servers 查看"
echo "  本机 hostname : $(hostname)"
echo "  token 归属    : $HOST（与实际 hostname 不同时，看板显示实际 hostname）"
`

func buildInstallScript(api, token, host string) string {
	script := installScriptTemplate
	script = strings.ReplaceAll(script, "__API__", api)
	script = strings.ReplaceAll(script, "__TOKEN__", token)
	script = strings.ReplaceAll(script, "__HOST__", host)
	return script
}

// FleetAgentInstall 返回可直接执行的安装脚本。
// 用该机器的 token 鉴权：只有拿到 token 的机器才能下载安装包。
func FleetAgentInstall(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.String(http.StatusUnauthorized, "missing token\n")
		return
	}
	agent, err := model.FindFleetAgentByToken(token)
	if err != nil {
		c.String(http.StatusUnauthorized, "invalid token\n")
		return
	}

	api := "http://" + c.Request.Host
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		api = "https://" + c.Request.Host
	}

	c.Header("Content-Type", "text/x-shellscript; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.String(http.StatusOK, buildInstallScript(api, token, agent.Hostname))
}

// FleetAgentBinary 下发采集端二进制。
func FleetAgentBinary(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.String(http.StatusUnauthorized, "missing token\n")
		return
	}
	if _, err := model.FindFleetAgentByToken(token); err != nil {
		c.String(http.StatusUnauthorized, "invalid token\n")
		return
	}

	path := agentBinaryPath
	if custom := os.Getenv("FLEET_AGENT_BIN"); custom != "" {
		path = custom
	}
	f, err := os.Open(path)
	if err != nil {
		c.String(http.StatusNotFound, "this build does not ship fleet-agent (%s)\n", path)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		c.String(http.StatusInternalServerError, "stat failed\n")
		return
	}

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", `attachment; filename="fleet-agent"`)
	c.Header("Content-Length", fmt.Sprintf("%d", stat.Size()))
	http.ServeContent(c.Writer, c.Request, "fleet-agent", stat.ModTime(), f)
}

// agentVersionCache 缓存二进制摘要，避免每次自更新检查都重算 6 MB
var agentVersionCache struct {
	sync.Once
	data map[string]any
}

// FleetAgentVersion 让采集端自检：服务端这版采集端是哪个二进制。
// 采集端对比自身 sha256，不一致就下载 /agent/bin 覆盖自己并重启。
func FleetAgentVersion(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.String(http.StatusUnauthorized, "missing token\n")
		return
	}
	if _, err := model.FindFleetAgentByToken(token); err != nil {
		c.String(http.StatusUnauthorized, "invalid token\n")
		return
	}

	agentVersionCache.Do(func() {
		path := agentBinaryPath
		if custom := os.Getenv("FLEET_AGENT_BIN"); custom != "" {
			path = custom
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			agentVersionCache.data = map[string]any{"available": false}
			return
		}
		sum := sha256.Sum256(raw)
		agentVersionCache.data = map[string]any{
			"available": true,
			"sha256":    hex.EncodeToString(sum[:]),
			"size":      len(raw),
		}
	})

	if agentVersionCache.data == nil || agentVersionCache.data["available"] != true {
		c.String(http.StatusNotFound, "this build does not ship fleet-agent\n")
		return
	}
	common.ApiSuccess(c, agentVersionCache.data)
}
