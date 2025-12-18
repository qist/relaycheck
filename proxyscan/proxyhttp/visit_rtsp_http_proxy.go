package proxyhttp

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"

	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/qist/relaycheck/config"
)

func TestRTSPViaHTTPProxy(proxyIP string, proxyPort int, rtspURL string, timeoutSeconds int, useHTTPS bool) (bool, string) {
	parsed, err := base.ParseURL(rtspURL)
	if err != nil {
		return false, fmt.Sprintf("解析RTSP地址失败: %v", err)
	}

	targetHost := parsed.Hostname()
	targetPort := "554"
	if p := parsed.Port(); p != "" {
		targetPort = p
	}
	targetAddr := net.JoinHostPort(targetHost, targetPort)

	proxyAddr := fmt.Sprintf("%s:%d", proxyIP, proxyPort)
	conn, err := net.DialTimeout("tcp", proxyAddr, time.Duration(timeoutSeconds)*time.Second)
	if err != nil {
		return false, fmt.Sprintf("连接HTTP代理失败: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Duration(timeoutSeconds) * time.Second))

	// 构造CONNECT请求
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", targetAddr, targetAddr)
	if config.Cfg.ProxyAuthEnabled {
		auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", config.Cfg.ProxyUsername, config.Cfg.ProxyPassword)))
		connectReq += fmt.Sprintf("Proxy-Authorization: Basic %s\r\n", auth)
	}
	connectReq += "\r\n"

	if _, err := conn.Write([]byte(connectReq)); err != nil {
		return false, fmt.Sprintf("发送CONNECT请求失败: %v", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		return false, fmt.Sprintf("读取CONNECT响应失败: %v", err)
	}
	if resp.StatusCode != 200 {
		return false, fmt.Sprintf("CONNECT失败，代理返回状态码: %d", resp.StatusCode)
	}

	// 检测伪代理（WebSocket）
	if resp.StatusCode == 101 && strings.Contains(strings.ToLower(resp.Header.Get("Upgrade")), "websocket") {
		return false, "伪代理: 返回 WebSocket 升级响应"
	}

	// 使用 gortsplib 客户端
	transport := gortsplib.TransportTCP
	client := &gortsplib.Client{
		Transport: &transport,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return conn, nil
		},
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	start := time.Now()
	errChan := make(chan error, 1)

	go func() {
		err := client.Start(parsed.Scheme, targetAddr)
		errChan <- err
	}()

	select {
	case <-ctx.Done():
		return false, "RTSP连接超时"
	case err := <-errChan:
		if err != nil {
			return false, fmt.Sprintf("RTSP握手失败: %v", err)
		}
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

	for _, m := range session.Medias {
		_, err := client.Setup(session.BaseURL, m, 0, 0)
		if err != nil {
			return false, fmt.Sprintf("SETUP失败: %v", err)
		}
	}

	_, err = client.Play(nil)
	if err != nil {
		return false, fmt.Sprintf("PLAY失败: %v", err)
	}

	duration := time.Since(start)
	return true, fmt.Sprintf("RTSP服务正常（HTTP%s代理转发），耗时: %v",
		func() string {
			if useHTTPS {
				return "S"
			}
			return ""
		}(), duration)
}
