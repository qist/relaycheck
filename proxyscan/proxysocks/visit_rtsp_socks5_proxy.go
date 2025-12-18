package proxysocks

import (
	"context"
	"fmt"
	"net"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/qist/relaycheck/config"
	"golang.org/x/net/proxy"
)

func TestRTSPViaSocks5(proxyIP string, proxyPort int, rtspURL string, timeoutSeconds int) (bool, string) {
	socksAddr := fmt.Sprintf("%s:%d", proxyIP, proxyPort)
	var auth *proxy.Auth
	if config.Cfg.ProxyAuthEnabled {
		auth = &proxy.Auth{
			User:     config.Cfg.ProxyUsername,
			Password: config.Cfg.ProxyPassword,
		}
	}
	socksDialer, err := proxy.SOCKS5("tcp", socksAddr, auth, proxy.Direct)
	if err != nil {
		return false, fmt.Sprintf("创建SOCKS5代理失败: %v", err)
	}

	parsed, err := base.ParseURL(rtspURL)
	if err != nil {
		return false, fmt.Sprintf("解析RTSP地址失败: %v", err)
	}

	host := parsed.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(parsed.Host, "554")
	}

	transport := gortsplib.TransportTCP
	client := gortsplib.Client{
		Transport: &transport,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return socksDialer.(proxy.ContextDialer).DialContext(ctx, network, addr)
		},
	}
	defer client.Close()

	// 连接
	err = client.Start(parsed.Scheme, host)
	if err != nil {
		return false, fmt.Sprintf("RTSP握手失败: %v", err)
	}

	session, _, err := client.Describe(parsed)
	if err != nil {
		return false, fmt.Sprintf("RTSP DESCRIBE失败: %v", err)
	}

	hasVideo := false
	for _, m := range session.Medias {
		for _, f := range m.Formats {
			switch f.(type) {
			case *format.H264, *format.H265, *format.VP8, *format.VP9, *format.MPEGTS:
				hasVideo = true
			}
		}
	}
	if !hasVideo {
		return false, "未发现支持的视频流（无 H264/H265/VP8/VP9，也无 MPEGTS）"
	}

	// 逐个 Setup，rtpPort 和 rtcpPort 都传0表示自动分配
	for _, m := range session.Medias {
		_, err := client.Setup(session.BaseURL, m, 0, 0)
		if err != nil {
			return false, fmt.Sprintf("SETUP失败: %v", err)
		}
	}

	// Play 不传参数
	_, err = client.Play(nil)
	if err != nil {
		return false, fmt.Sprintf("PLAY失败: %v", err)
	}
	return true, "RTSP服务正常"
}
