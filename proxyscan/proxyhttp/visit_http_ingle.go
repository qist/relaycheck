package proxyhttp
import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"crypto/tls"

	"bufio"
	"encoding/base64"
	"io"
	"github.com/qist/relaycheck/config"
	"github.com/qist/relaycheck/responsechecker"

)

func VisitHTTPSingleRequest(proxyType, ip string, port int, targetURL string, uaHeaders map[string][]string, httpProxy *config.HttpProxyConfig, timeoutSeconds int, validateContent bool, raw bool) (bool, string) {
	if strings.HasPrefix(targetURL, "rtsp://") {
		return TestRTSPViaHTTPProxy(ip, port, targetURL, timeoutSeconds, proxyType == "https")
	}

	proxyAddr := fmt.Sprintf("%s:%d", ip, port)
	proxyScheme := "http"
	if strings.ToLower(proxyType) == "https" {
		proxyScheme = "https"
	}

	var transport *http.Transport
	if httpProxy != nil && httpProxy.Enabled && len(httpProxy.Headers) > 0 {
		dialer := &httpProxyDialer{
			proxyAddr: proxyAddr,
			headers:   httpProxy.Headers,
		}
		transport = &http.Transport{
			DialContext:           dialer.DialContext,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			ResponseHeaderTimeout: time.Duration(timeoutSeconds) * time.Second,
			ExpectContinueTimeout: time.Duration(timeoutSeconds) * time.Second,
			IdleConnTimeout:       1 * time.Second,
			TLSHandshakeTimeout:   time.Duration(timeoutSeconds) * time.Second,
			DisableKeepAlives:     true,
		}
	} else {
		proxyURL, err := url.Parse(fmt.Sprintf("%s://%s", proxyScheme, proxyAddr))
		if err != nil {
			return false, fmt.Sprintf("解析代理地址失败: %v", err)
		}
		transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&net.Dialer{
				Timeout:   time.Duration(timeoutSeconds) * time.Second,
				KeepAlive: time.Duration(timeoutSeconds) * time.Second,
			}).DialContext,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			ResponseHeaderTimeout: time.Duration(timeoutSeconds) * time.Second,
			ExpectContinueTimeout: time.Duration(timeoutSeconds) * time.Second,
			IdleConnTimeout:       1 * time.Second,
			TLSHandshakeTimeout:   time.Duration(timeoutSeconds) * time.Second,
			DisableKeepAlives:     true,
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(timeoutSeconds) * time.Second,
	}

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return false, fmt.Sprintf("构造请求失败: %v", err)
	}
	for k, vs := range uaHeaders {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	// 添加代理认证头
	if config.Cfg.ProxyAuthEnabled {
		auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", config.Cfg.ProxyUsername, config.Cfg.ProxyPassword)))
		req.Header.Set("Proxy-Authorization", "Basic "+auth)
	}

	resp, err := client.Do(req)
	if err != nil {
		transport.CloseIdleConnections()
		return false, fmt.Sprintf("HTTP代理请求失败: %v", err)
	}
	defer resp.Body.Close()
	defer transport.CloseIdleConnections()

	if resp.StatusCode == 101 && strings.Contains(strings.ToLower(resp.Header.Get("Upgrade")), "websocket") {
		return false, "伪代理: 返回 WebSocket 升级响应"
	}
	if !validateContent || responsechecker.IsMediaURL(targetURL) {
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound {
			return true, fmt.Sprintf("状态码: %d，未验证内容", resp.StatusCode)
		}
		return false, fmt.Sprintf("状态码: %d，未验证内容", resp.StatusCode)
	}

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := resp.Header.Get("Location")
		if location != "" {
			redirectReq, err := http.NewRequest("GET", location, nil)
			if err != nil {
				return false, fmt.Sprintf("构造跳转请求失败: %v", err)
			}
			for k, vs := range uaHeaders {
				for _, v := range vs {
					redirectReq.Header.Add(k, v)
				}
			}
			if httpProxy != nil && httpProxy.Enabled && httpProxy.Headers != nil {
				for k, v := range httpProxy.Headers {
					if strings.ToLower(k) == "host" {
						redirectReq.Host = v
					} else {
						redirectReq.Header.Set(k, v)
					}
				}
			}
			redirectResp, err := client.Do(redirectReq)
			if err != nil {
				return false, fmt.Sprintf("跳转请求失败: %v", err)
			}
			defer redirectResp.Body.Close()
			body, err := io.ReadAll(redirectResp.Body)
			if err != nil {
				return false, fmt.Sprintf("读取跳转响应失败: %v", err)
			}
			return responsechecker.CheckResponse(redirectResp, body, validateContent, raw)
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Sprintf("读取响应失败: %v", err)
	}
	return responsechecker.CheckResponse(resp, body, validateContent, raw)
}

func (d *httpProxyDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.Dial(network, addr)
}

// httpProxyDialer 实现 proxy.Dialer 接口
type httpProxyDialer struct {
	proxyAddr string
	headers   map[string]string
}

func (d *httpProxyDialer) Dial(network, addr string) (net.Conn, error) {
	// 1️⃣ 连接代理服务器
	conn, err := net.DialTimeout(network, d.proxyAddr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("连接代理失败: %v", err)
	}

	// 2️⃣ 构造 CONNECT 请求
	var req strings.Builder
	req.WriteString(fmt.Sprintf("CONNECT %s HTTP/1.1\r\n", addr))

	// 3️⃣ 添加自定义请求头
	if len(d.headers) > 0 {
		for k, v := range d.headers {
			req.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
		}
	}

	// 4️⃣ 添加 Proxy-Authorization 认证头（如果全局开启）
	if config.Cfg.ProxyAuthEnabled {
		auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", config.Cfg.ProxyUsername, config.Cfg.ProxyPassword)))
		req.WriteString(fmt.Sprintf("Proxy-Authorization: Basic %s\r\n", auth))
	}

	// 5️⃣ 默认 Host
	if len(d.headers) == 0 {
		req.WriteString(fmt.Sprintf("Host: %s\r\n", addr))
	}

	req.WriteString("\r\n") // 结束头部

	// 6️⃣ 发送 CONNECT 请求
	if _, err := conn.Write([]byte(req.String())); err != nil {
		conn.Close()
		return nil, fmt.Errorf("发送 CONNECT 请求失败: %v", err)
	}

	// 7️⃣ 读取代理响应
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("读取 CONNECT 响应失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("HTTP 代理 CONNECT 失败: %s", resp.Status)
	}

	// 8️⃣ 返回隧道连接
	return conn, nil
}