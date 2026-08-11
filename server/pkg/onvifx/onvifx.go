package onvifx

import (
	"context"
	"io"
	"log"
	"net"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	onvif "github.com/use-go/onvif"
	"github.com/use-go/onvif/media"
	xsdonvif "github.com/use-go/onvif/xsd/onvif"
)

type Found struct {
	XAddr string `json:"xaddr"`
	IP    string `json:"ip"`
	Port  int    `json:"port"`
	Name  string `json:"name"`
}

func Discover(ctx context.Context, iface string) ([]Found, error) {
	names, err := ifaceNames(iface)
	if err != nil {
		return nil, err
	}
	seen := map[string]Found{}
	var out []Found
	for _, name := range names {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		devs, err := onvif.GetAvailableDevicesAtSpecificEthernetInterface(name)
		if err != nil {
			continue
		}
		for _, d := range devs {
			params := d.GetDeviceParams()
			host, port := parseHostPort(params.Xaddr)
			if host == "" {
				continue
			}
			key := net.JoinHostPort(host, strconv.Itoa(port))
			if _, dup := seen[key]; dup {
				continue
			}
			f := Found{XAddr: params.Xaddr, IP: host, Port: port}
			if info := d.GetDeviceInfo(); info.Model != "" {
				f.Name = info.Model
			} else {
				f.Name = host
			}
			seen[key] = f
			out = append(out, f)
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			default:
			}
		}
	}
	return out, nil
}

// ifaceNames resolves the target interface(s) for discovery. When name is
// empty, all non-loopback interfaces with an IPv4 address are used so that
// WS-Discovery broadcasts go out on every active LAN adapter.
func ifaceNames(name string) ([]string, error) {
	if name != "" {
		if _, err := net.InterfaceByName(name); err != nil {
			return nil, err
		}
		return []string{name}, nil
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				out = append(out, i.Name)
				break
			}
		}
	}
	return out, nil
}

// GetStreamURI fetches the RTSP URL via ONVIF Media service (best effort).
func GetStreamURI(host string, port int, user, pass string) string {
	if port == 0 {
		port = 80
	}
	dev, err := onvif.NewDevice(onvif.DeviceParams{
		Xaddr:    net.JoinHostPort(host, strconv.Itoa(port)),
		Username: user,
		Password: pass,
	})
	if err != nil {
		return ""
	}
	resp, err := dev.CallMethod(media.GetProfiles{})
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	doc := etree.NewDocument()
	if err := doc.ReadFromString(string(body)); err != nil {
		return ""
	}
	var token string
	for _, p := range doc.FindElements("./Envelope/Body/GetProfilesResponse/Profiles/Profile") {
		if t := p.SelectAttrValue("token", ""); t != "" {
			token = t
			break
		}
	}
	if token == "" {
		return ""
	}
	resp2, err := dev.CallMethod(media.GetStreamUri{
		StreamSetup: xsdonvif.StreamSetup{
			Stream:    xsdonvif.StreamType("RTP-Unicast"),
			Transport: xsdonvif.Transport{Protocol: xsdonvif.TransportProtocol("RTSP")},
		},
		ProfileToken: xsdonvif.ReferenceToken(token),
	})
	if err != nil {
		return ""
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	doc2 := etree.NewDocument()
	if err := doc2.ReadFromString(string(body2)); err != nil {
		return ""
	}
	for _, u := range doc2.FindElements("./Envelope/Body/GetStreamUriResponse/MediaUri/Uri") {
		uri := strings.TrimSpace(u.Text())
		if strings.HasPrefix(uri, "rtsp://") {
			return uri
		}
	}
	return ""
}

func parseHostPort(xaddr string) (string, int) {
	x := xaddr
	if i := strings.Index(x, "://"); i >= 0 {
		x = x[i+3:]
	}
	if i := strings.Index(x, "/"); i >= 0 {
		x = x[:i]
	}
	if x == "" {
		return "", 0
	}
	host, portStr, err := net.SplitHostPort(x)
	if err != nil {
		return x, 80
	}
	port, _ := strconv.Atoi(portStr)
	if port == 0 {
		port = 80
	}
	return host, port
}

func DebugLog(format string, args ...any) {
	log.Printf(format, args...)
}
