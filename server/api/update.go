package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 更新检查。数据源为 GitHub Releases（https://github.com/upcyan/CyanNVR/releases）。
//
// fpk（fnOS 应用）不做应用内自更新：appcenter-cli 的 install-fpk 需要 root，
// 而应用进程以受限用户 cyannvr 运行，无法直接安装。因此这里只做「检查 + 引导」，
// 前端发现新版本后跳转到 GitHub release 页面，由用户自行下载安装包、在应用中心手动安装。
// Android App 的自更新在 App 端实现（下载 APK + 调起系统安装器）。

const updateRepoAPI = "https://api.github.com/repos/upcyan/CyanNVR/releases/latest"

type updateCheckResp struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"hasUpdate"`
	URL       string `json:"url,omitempty"` // release 页面地址，供前端跳转
	Notes     string `json:"notes,omitempty"`
	Error     string `json:"error,omitempty"`
}

// versionCmp 比较 x.y.z 形式版本，返回 -1/0/1。
func versionCmp(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(strings.TrimSpace(a), "v"), ".")
	pb := strings.Split(strings.TrimPrefix(strings.TrimSpace(b), "v"), ".")
	for i := 0; i < 3; i++ {
		var na, nb int
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &na)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &nb)
		}
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
	}
	return 0
}

func (s *Server) checkUpdate(c *gin.Context) {
	current := strings.TrimPrefix(CoreVersion, "v")
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, updateRepoAPI, nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"update": updateCheckResp{Current: current, Error: err.Error()}})
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CyanNVR")
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"update": updateCheckResp{
			Current: current,
			Error:   "检查更新失败（无法访问 GitHub）：" + err.Error(),
		}})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusOK, gin.H{"update": updateCheckResp{
			Current: current,
			Error:   fmt.Sprintf("检查更新失败（GitHub 返回 %d）", resp.StatusCode),
		}})
		return
	}
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		c.JSON(http.StatusOK, gin.H{"update": updateCheckResp{Current: current, Error: err.Error()}})
		return
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	out := updateCheckResp{
		Current:   current,
		Latest:    latest,
		HasUpdate: versionCmp(latest, current) > 0,
		URL:       rel.HTMLURL,
		Notes:     strings.TrimSpace(rel.Body),
	}
	c.JSON(http.StatusOK, gin.H{"update": out})
}
