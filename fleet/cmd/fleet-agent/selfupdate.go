/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package main

// 采集端自更新：
// 服务端内置了采集端二进制（/fleet-agent-bin），这里定期对比 sha256，
// 不一致就下载覆盖自己并重启服务。这样升级只需换服务端镜像，
// 所有采集端会在一个周期内自动跟上，不用登录每台机器。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	updateCheckInterval = 6 * time.Hour
	updateFirstDelay    = 45 * time.Second
	updateMinSize       = 1 << 20 // 小于 1MB 的包肯定不对
)

type agentVersionInfo struct {
	Available bool   `json:"available"`
	Sha256    string `json:"sha256"`
	Size      int64  `json:"size"`
}

// selfUpdater 每台采集端一个，记录下次检查时间
type selfUpdater struct {
	next time.Time
}

func newSelfUpdater() *selfUpdater {
	return &selfUpdater{next: time.Now().Add(updateFirstDelay)}
}

// maybeUpdate 到点就检查一次；返回 true 表示已经替换了二进制（调用方应重启）
func (u *selfUpdater) maybeUpdate() bool {
	if time.Now().Before(u.next) {
		return false
	}
	u.next = time.Now().Add(updateCheckInterval)

	if !*autoUpdate || *simulate != "" || *once || *dryRun || *token == "" {
		return false
	}

	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	mine, err := fileSHA256(exe)
	if err != nil {
		if *verbose {
			log.Printf("自更新：读不到自身二进制 %v", err)
		}
		return false
	}

	info, err := fetchAgentVersion()
	if err != nil {
		if *verbose {
			log.Printf("自更新：查询服务端版本失败 %v", err)
		}
		return false
	}
	if !info.Available || info.Sha256 == "" || info.Sha256 == mine {
		if *verbose {
			log.Printf("自更新：已是最新 (%s)", mine[:8])
		}
		return false
	}

	log.Printf("自更新：服务端有新版本 %s，开始替换…", info.Sha256[:8])
	if err := installUpdate(exe, info); err != nil {
		log.Printf("自更新失败：%v（继续用旧版本运行）", err)
		return false
	}
	log.Printf("自更新：二进制已替换，准备重启")
	return true
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fleetEndpoint(path string) string {
	return strings.TrimRight(*serverURL, "/") + path
}

func fetchAgentVersion() (*agentVersionInfo, error) {
	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/api/fleet/agent/version?token=%s", strings.TrimRight(*serverURL, "/"), *token), nil)
	if err != nil {
		return nil, err
	}
	cl := &http.Client{Timeout: 20 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var env struct {
		Success bool             `json:"success"`
		Data    agentVersionInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func downloadAgentBinary(dst string) error {
	url := fmt.Sprintf("%s/api/fleet/agent/bin?token=%s", strings.TrimRight(*serverURL, "/"), *token)
	cl := &http.Client{Timeout: 3 * time.Minute}
	resp, err := cl.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(dst)
		return err
	}
	return f.Close()
}

// installUpdate 下载 → 校验 → 备份旧的 → 原子替换
func installUpdate(exe string, info *agentVersionInfo) error {
	tmp := exe + ".new"
	if err := downloadAgentBinary(tmp); err != nil {
		return err
	}
	defer os.Remove(tmp)

	stat, err := os.Stat(tmp)
	if err != nil {
		return err
	}
	if stat.Size() < updateMinSize || (info.Size > 0 && stat.Size() != info.Size) {
		return fmt.Errorf("下载大小异常：%d 字节", stat.Size())
	}

	sum, err := fileSHA256(tmp)
	if err != nil {
		return err
	}
	if info.Sha256 != "" && sum != info.Sha256 {
		return fmt.Errorf("校验不一致：期望 %s，实际 %s", info.Sha256[:8], sum[:8])
	}

	// 备份旧的可执行文件，替换失败还能手动恢复
	_ = os.Rename(exe, exe+".bak")
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(exe+".bak", exe) // 回滚
		return err
	}
	_ = os.Chmod(exe, 0o755)
	return nil
}

// restartSelf 在 systemd 下由服务管理器重启；否则提示手动重启
func restartSelf() {
	if os.Getenv("INVOCATION_ID") != "" {
		if err := exec.Command("systemctl", "restart", "fleet-agent").Run(); err != nil {
			log.Printf("自更新：systemctl restart 失败 %v，退出等待拉起", err)
		}
		os.Exit(0)
	}
	log.Printf("自更新：当前不是 systemd 托管，请手动重启采集端")
}
