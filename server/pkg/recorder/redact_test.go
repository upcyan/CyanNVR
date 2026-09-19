package recorder

import "testing"

// TestRedactText 确保 ffmpeg stderr 里的摄像头密码不会明文进日志。
func TestRedactText(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			name: "ffmpeg 真实报错格式",
			in:   "rtsp://admin:tpl-184766@192.168.68.56:554/stream1: Invalid data found when processing input",
			want: "rtsp://admin:****@192.168.68.56:554/stream1: Invalid data found when processing input",
		},
		{
			name: "URL 编码的密码",
			in:   "http://user:p%40ss@cam.local/onvif",
			want: "http://user:****@cam.local/onvif",
		},
		{
			name: "多行 stderr 中的 URL",
			in:   "[rtsp @ 0x55f] Could not find codec parameters\nrtsp://a:b@1.2.3.4/x",
			want: "[rtsp @ 0x55f] Could not find codec parameters\nrtsp://a:****@1.2.3.4/x",
		},
		{
			name: "一行内多个 URL",
			in:   "rtsp://u1:p1@10.0.0.1/a and rtsp://u2:p2@10.0.0.2/b",
			want: "rtsp://u1:****@10.0.0.1/a and rtsp://u2:****@10.0.0.2/b",
		},
		{
			name: "无凭证 URL 保持原样",
			in:   "rtsp://192.168.1.10:554/stream1: timeout",
			want: "rtsp://192.168.1.10:554/stream1: timeout",
		},
		{name: "无 URL", in: "no url here", want: "no url here"},
		{name: "空串", in: "", want: ""},
	}
	for _, c := range cases {
		if got := redactText(c.in); got != c.want {
			t.Errorf("%s:\n  redactText(%q)\n  = %q\n  want %q", c.name, c.in, got, c.want)
		}
	}
}
