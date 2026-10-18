package api

import (
	"regexp"
	"strings"
)

// 三条发布线的版本是**独立**的，不能共用一个数字：
//
//	web    —— 前端静态产物（__APP_VERSION__ / build.json），随服务器版本走
//	fpk    —— fnOS 安装包，版本形如 1.10.34-6（核心版本-打包修订号）
//	android—— 安卓 App，版本形如 1.10.33（versionName）
//
// 历史上它们被当成同一个 tag 版本：检查更新只看 release 的 tag_name，
// 而 tag 由「最后一次发布的动作」决定。只要某次只发了其中一个平台，
// 另外两端的用户就会看到「有新版本」→ 下载 → 装完版本号没变 → 再检查还是新版本，
// 形成无限提示（实测 v1.10.35 的 tag 下挂着 1.10.34-6 的 fpk 与 1.10.33 的 apk）。
//
// 因此版本必须从**资产文件名**解析：谁的就是谁的，比对这个才对得上。

// fpkVersionRe 匹配 CyanNVR_<core>-<rev>_x86.fpk
var fpkVersionRe = regexp.MustCompile(`CyanNVR_(\d+\.\d+\.\d+)-(\d+)_x86\.fpk$`)

// androidVersionRe 匹配 CyanNVR_<ver>_android.apk
var androidVersionRe = regexp.MustCompile(`CyanNVR_(\d+\.\d+\.\d+)_android\.apk$`)

// releaseAsset 是 release 里的一个附件。
type releaseAsset struct {
	Name string `json:"name"` // 文件名，版本从这里解析
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// platformVersions 从一次 release 的资产列表里提取各平台版本。
// 缺失的平台留空（该平台没有发布物），调用方据此决定「无更新」还是「无法判断」。
type platformVersions struct {
	Fpk     string // 1.10.34-6
	Android string // 1.10.33
}

// parsePlatformVersions 从资产文件名解析各平台版本；同名多份时取版本最高者。
func parsePlatformVersions(assets []releaseAsset) platformVersions {
	var out platformVersions
	for _, a := range assets {
		if m := fpkVersionRe.FindStringSubmatch(a.Name); m != nil {
			v := m[1] + "-" + m[2]
			if compareReleaseVersion(v, out.Fpk) > 0 {
				out.Fpk = v
			}
			continue
		}
		if m := androidVersionRe.FindStringSubmatch(a.Name); m != nil {
			if compareReleaseVersion(m[1], out.Android) > 0 {
				out.Android = m[1]
			}
		}
	}
	return out
}

// compareReleaseVersion 比较版本号，支持可选的「-修订号」后缀。
// 主版本按 x.y.z 数值比较；主版本相同时，有修订号的一侧更大（1.10.34-6 > 1.10.34）。
func compareReleaseVersion(a, b string) int {
	if b == "" {
		if a == "" {
			return 0
		}
		return 1
	}
	if a == "" {
		return -1
	}
	coreA, revA := splitRevision(a)
	coreB, revB := splitRevision(b)
	if c := compareCore(coreA, coreB); c != 0 {
		return c
	}
	switch {
	case revA > revB:
		return 1
	case revA < revB:
		return -1
	}
	return 0
}

func splitRevision(v string) (string, int) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '-'); i >= 0 {
		rev := 0
		for _, c := range v[i+1:] {
			if c < '0' || c > '9' {
				return v[:i], 0
			}
			rev = rev*10 + int(c-'0')
		}
		return v[:i], rev
	}
	return v, 0
}

func compareCore(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		na := atoiOrZero(pa, i)
		nb := atoiOrZero(pb, i)
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
	}
	return 0
}

func atoiOrZero(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n := 0
	for _, c := range parts[i] {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
