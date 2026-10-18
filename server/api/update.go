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
//
// 关键：三条线（web/fpk/android）**各自独立比对**。见 server/release.go 的说明——
// 用 release 的 tag_name 比对任意一端都会错，因为 tag 由最后一次发布动作决定。

const updateRepoAPI = "https://api.github.com/repos/upcyan/CyanNVR/releases/latest"

type updateCheckResp struct {
	Current   string `json:"current"` // 当前端自己的版本
	Latest    string `json:"latest"`  // 该端最新可用版本
	HasUpdate bool   `json:"hasUpdate"`
	URL       string `json:"url,omitempty"` // release 页面地址，供前端跳转
	Notes     string `json:"notes,omitempty"`
	Error     string `json:"error,omitempty"`
	// Platform 说明此次比对的是哪一端（web/fpk/android），便于排查「查到更新却装不上」。
	Platform string `json:"platform,omitempty"`
}

// versionCmp 比较 x.y.z 形式版本，返回 -1/0/1。
// 保留给既有调用方；新代码请用 compareReleaseVersion（支持 -修订号）。
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

// checkUpdate 返回指定平台的更新状态。
//
// query platform:
//   - fpk     与 release 里 fpk 资产版本比对（fnOS 部署）
//   - android 与 apk 资产版本比对（App 内嵌场景由原生桥自查，这里主要给 Web 调试用）
//   - web     与服务器核心版本比对（纯浏览器访问时的「界面版本」，随 fpk 一起更新）
//     缺省按 fpk 处理（fnOS 部署是主线）。
func (s *Server) checkUpdate(c *gin.Context) {
	platform := strings.ToLower(strings.TrimSpace(c.Query("platform")))
	if platform == "" {
		platform = "fpk"
	}
	// 当前版本必须与该平台的可安装产物同一形态：
	//   fpk  ——「1.10.34-6」（含修订号），否则拿 1.10.34 去比 1.10.34-6 永远「有更新」；
	//   android/web —— 纯 x.y.z。
	current := strings.TrimPrefix(CoreVersion, "v")
	if platform == "fpk" && FpkVersion != "" {
		current = strings.TrimPrefix(FpkVersion, "v")
	}
	out := updateCheckResp{Current: current, Platform: platform}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, updateRepoAPI, nil)
	if err != nil {
		out.Error = err.Error()
		c.JSON(http.StatusOK, gin.H{"update": out})
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CyanNVR")
	resp, err := client.Do(req)
	if err != nil {
		out.Error = "检查更新失败（无法访问 GitHub）：" + err.Error()
		c.JSON(http.StatusOK, gin.H{"update": out})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		out.Error = fmt.Sprintf("检查更新失败（GitHub 返回 %d）", resp.StatusCode)
		c.JSON(http.StatusOK, gin.H{"update": out})
		return
	}
	var rel struct {
		TagName string         `json:"tag_name"`
		HTMLURL string         `json:"html_url"`
		Body    string         `json:"body"`
		Assets  []releaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		out.Error = err.Error()
		c.JSON(http.StatusOK, gin.H{"update": out})
		return
	}

	versions := parsePlatformVersions(rel.Assets)
	var latest string
	switch platform {
	case "android":
		latest = versions.Android
	default:
		latest = versions.Fpk
	}

	out.URL = rel.HTMLURL
	out.Notes = strings.TrimSpace(rel.Body)
	if latest == "" {
		// 该平台在这一版 release 里没有产物：不是「已是最新」，也不能报「有更新」，
		// 否则用户点了也下载不到对应安装包（这正是此前死循环的一半成因）。
		out.Error = "该版本暂未发布 " + platform + " 安装包，请稍后再试"
		c.JSON(http.StatusOK, gin.H{"update": out})
		return
	}
	out.Latest = latest
	out.HasUpdate = compareReleaseVersion(latest, current) > 0
	c.JSON(http.StatusOK, gin.H{"update": out})
}
