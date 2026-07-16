package adapter

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func (a *App) handleSystemUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, updateStatus())
}

func (a *App) handleSystemUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.adminAuthorized(w, r) {
		http.Error(w, "未登录：请使用用户名密码登录后台", http.StatusUnauthorized)
		return
	}
	url := strings.TrimSpace(os.Getenv("ADAPTER_UPDATE_URL"))
	token := strings.TrimSpace(os.Getenv("ADAPTER_UPDATE_TOKEN"))
	if url == "" || token == "" {
		http.Error(w, "在线更新尚未配置：需要独立更新器地址和访问令牌", http.StatusServiceUnavailable)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "触发更新失败："+err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		http.Error(w, fmt.Sprintf("更新器返回 %d：%s", resp.StatusCode, strings.TrimSpace(string(body))), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"message": "更新任务已提交。若仓库存在新镜像，服务会自动重启。",
		"status":  updateStatus(),
	})
}

func updateStatus() map[string]any {
	url := strings.TrimSpace(os.Getenv("ADAPTER_UPDATE_URL"))
	token := strings.TrimSpace(os.Getenv("ADAPTER_UPDATE_TOKEN"))
	image := strings.TrimSpace(os.Getenv("ADAPTER_IMAGE"))
	if image == "" {
		image = "未配置"
	}
	channel := strings.TrimSpace(os.Getenv("ADAPTER_UPDATE_CHANNEL"))
	if channel == "" {
		channel = "latest"
	}
	return map[string]any{
		"configured": url != "" && token != "",
		"image":      image,
		"channel":    channel,
		"version":    VersionInfo(),
	}
}
