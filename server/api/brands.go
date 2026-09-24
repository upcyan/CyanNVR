package api

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// brandTemplate 是一个品牌的 RTSP 地址模板。
//
// main/sub 是路径模板，其中 {ch} 会被通道号替换。这样接在 NVR 后面的
// 通道（海康 101/201/301、大华 channel=1/2/3）不用用户手算地址。
type brandTemplate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	NeedCh   bool   `json:"needCh"`   // 是否需要用户填通道号
	ChHint   string `json:"chHint"`   // 通道号填写提示
	MainPath string `json:"mainPath"` // 主码流路径模板
	SubPath  string `json:"subPath"`  // 子码流路径模板（空 = 无子码流）
	Note     string `json:"note"`     // 补充说明
}

// brandTemplates 覆盖常见品牌。默认给「自动探测」，把逐条试路径交给后端，
// 用户无需知道自己的相机属于哪一类。
var brandTemplates = []brandTemplate{
	{
		ID: "auto", Name: "自动探测（推荐）",
		Note: "依次尝试各品牌常见地址，成功即用；不知道型号时选这个",
	},
	{
		ID: "hik", Name: "海康威视 / 萤石",
		NeedCh: true, ChHint: "普通摄像头填 1；接在 NVR 上按通道顺序填 1、2、3…",
		MainPath: "/Streaming/Channels/{ch}01",
		SubPath:  "/Streaming/Channels/{ch}02",
		Note:     "通道 1 → 101/102，通道 2 → 201/202",
	},
	{
		ID: "dahua", Name: "大华",
		NeedCh: true, ChHint: "普通摄像头填 1；接在 NVR 上按通道顺序填 1、2、3…",
		MainPath: "/cam/realmonitor?channel={ch}&subtype=0",
		SubPath:  "/cam/realmonitor?channel={ch}&subtype=1",
	},
	{
		ID: "uniview", Name: "宇视",
		MainPath: "/media/video1",
		SubPath:  "/media/video2",
	},
	{
		ID: "huawei", Name: "华为",
		MainPath: "/LiveMedia/ch1/Media1",
		SubPath:  "/LiveMedia/ch1/Media2",
	},
	{
		ID: "tplink", Name: "TP-LINK",
		MainPath: "/stream1",
		SubPath:  "/stream2",
	},
	{
		ID: "xiaomi", Name: "小米",
		MainPath: "/live/ch00_0",
		SubPath:  "/live/ch01_0",
		Note:     "小米摄像头通常需在 App 里开启「RTSP」并设置独立密码",
	},
	{
		ID: "custom", Name: "自定义 RTSP 地址",
		Note: "直接填写完整 RTSP 地址（主码流必填，子码流可留空）",
	},
}

// rtspBrands 返回品牌模板列表，供「添加设备」界面的品牌下拉使用。
func (s *Server) rtspBrands(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"brands": brandTemplates})
}

// rtspBuildReq 是 /devices/rtsp-url 的入参：按品牌+通道号拼出 RTSP 地址。
type rtspBuildReq struct {
	Brand    string `json:"brand"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Channel  int    `json:"channel"`
}

// buildBrandRTSP 按品牌模板生成主/子码流地址。
func buildBrandRTSP(req rtspBuildReq) gin.H {
	port := req.Port
	if port == 0 {
		port = 554
	}
	ch := req.Channel
	if ch <= 0 {
		ch = 1
	}
	var tpl *brandTemplate
	for i := range brandTemplates {
		if brandTemplates[i].ID == req.Brand {
			tpl = &brandTemplates[i]
			break
		}
	}
	if tpl == nil || tpl.ID == "auto" || tpl.ID == "custom" {
		// auto/custom 不由这里拼地址：auto 交给 testDevice 逐条试，
		// custom 由用户直接填完整地址。
		return gin.H{"main": "", "sub": ""}
	}
	host := net.JoinHostPort(req.IP, strconv.Itoa(port))
	fill := func(p string) string {
		if p == "" {
			return ""
		}
		p = strings.ReplaceAll(p, "{ch}", strconv.Itoa(ch))
		u := url.URL{Scheme: "rtsp", Host: host}
		// 路径里可能带 query（大华的 ?channel=1&subtype=0）
		if i := strings.IndexByte(p, '?'); i >= 0 {
			u.Path = p[:i]
			u.RawQuery = p[i+1:]
		} else {
			u.Path = p
		}
		if req.Username != "" {
			u.User = url.UserPassword(req.Username, req.Password)
		}
		return u.String()
	}
	return gin.H{"main": fill(tpl.MainPath), "sub": fill(tpl.SubPath)}
}

// rtspURLForBrand 是 /devices/rtsp-url 的处理函数：
// 让前端在选定品牌/通道后立刻看到将要使用的地址，避免「我填的对不对」的疑惑。
func (s *Server) rtspURLForBrand(c *gin.Context) {
	var req rtspBuildReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if req.IP == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "需要 IP 地址"})
		return
	}
	res := buildBrandRTSP(req)
	c.JSON(http.StatusOK, res)
}
