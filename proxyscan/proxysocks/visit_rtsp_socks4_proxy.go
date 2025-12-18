package proxysocks

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
)

func TestRTSPViaSocks4a(proxyIP string, proxyPort int, rtspURL string, timeoutSeconds int) (bool, string) {
	parsed, err := base.ParseURL(rtspURL)
	if err != nil {
		return false, fmt.Sprintf("解析RTSP地址失败: %v", err)
	}

	// 补默认端口
	host := parsed.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(parsed.Host, "554")
	}

	// 建立 SOCKS4A 代理 TCP 连接
	proxyAddr := fmt.Sprintf("%s:%d", proxyIP, proxyPort)
	conn, err := net.DialTimeout("tcp", proxyAddr, time.Duration(timeoutSeconds)*time.Second)
	if err != nil {
		return false, fmt.Sprintf("连接SOCKS4A代理失败: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Duration(timeoutSeconds) * time.Second))

	targetHost, targetPort, err := net.SplitHostPort(host)
	if err != nil {
		return false, fmt.Sprintf("解析目标地址失败: %v", err)
	}
	portNum, err := strconv.Atoi(targetPort)
	if err != nil {
		return false, fmt.Sprintf("解析目标端口失败: %v", err)
	}

	// 构造 SOCKS4A 请求包（连接请求）
	req := []byte{
		0x04, 0x01, // SOCKS4 CONNECT command
		byte(portNum >> 8), byte(portNum & 0xff), // 目标端口高低字节
		0x00, 0x00, 0x00, 0x01, // 0.0.0.1 表示域名地址
		0x00, // USERID 为空
	}
	req = append(req, []byte(targetHost)...)
	req = append(req, 0x00)

	_, err = conn.Write(req)
	if err != nil {
		return false, fmt.Sprintf("发送SOCKS4A请求失败: %v", err)
	}

	resp := make([]byte, 8)
	_, err = io.ReadFull(conn, resp)
	if err != nil {
		return false, fmt.Sprintf("读取SOCKS4A响应失败: %v", err)
	}

	if resp[1] != 0x5A { // 0x5A 表示请求成功
		return false, fmt.Sprintf("SOCKS4A连接失败，响应码: %d", resp[1])
	}

	// 连接成功后，使用该连接进行 RTSP Client 操作
	transport := gortsplib.TransportTCP
	client := &gortsplib.Client{
		Transport: &transport,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// 直接复用建立的 SOCKS4A 连接
			return conn, nil
		},
	}
	defer client.Close()

	// RTSP 连接启动
	err = client.Start(parsed.Scheme, host)
	if err != nil {
		return false, fmt.Sprintf("RTSP握手失败: %v", err)
	}

	// Describe 获取媒体信息
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

	// Setup
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

	return true, "RTSP服务正常（SOCKS4A）"
}
