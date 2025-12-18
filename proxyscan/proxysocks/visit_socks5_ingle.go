package proxysocks
import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"io"
	"time"
	"crypto/tls"
	"github.com/qist/relaycheck/config"
	"github.com/qist/relaycheck/responsechecker"
)
func VisitSocks5SingleRequest(ip string, port int, urlToTest string, uaHeaders map[string][]string, timeout int, validateContent bool, raw bool) (bool, string) {
	if strings.HasPrefix(urlToTest, "rtsp://") {
		return TestRTSPViaSocks5(ip, port, urlToTest, timeout)
	}
	var proxyAddr string
	if config.Cfg.ProxyAuthEnabled {
		proxyAddr = fmt.Sprintf("socks5://%s:%s@%s:%d", config.Cfg.ProxyUsername, config.Cfg.ProxyPassword, ip, port)
	} else {
		proxyAddr = fmt.Sprintf("socks5://%s:%d", ip, port)
	}
	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return false, fmt.Sprintf("解析SOCKS5代理URL失败: %v", err)
	}

	testURL, err := url.Parse(urlToTest)
	if err != nil {
		return false, fmt.Sprintf("解析目标URL失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		DialContext: (&net.Dialer{
			Timeout:   time.Duration(timeout/3) * time.Second,
			KeepAlive: 1 * time.Second,
			DualStack: true,
		}).DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		ResponseHeaderTimeout: time.Duration(timeout/2) * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
		IdleConnTimeout:       1 * time.Second,
		TLSHandshakeTimeout:   time.Duration(timeout/3) * time.Second,
		DisableKeepAlives:     true,
		MaxIdleConns:          0,
		MaxIdleConnsPerHost:   0,
		ForceAttemptHTTP2:     false,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(timeout) * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlToTest, nil)
	if err != nil {
		transport.CloseIdleConnections()
		return false, fmt.Sprintf("构造请求失败: %v", err)
	}
	req.Host = testURL.Host
	for k, vs := range uaHeaders {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	done := make(chan struct{})
	var resp *http.Response
	var reqErr error

	go func() {
		resp, reqErr = client.Do(req)
		close(done)
	}()

	select {
	case <-ctx.Done():
		transport.CloseIdleConnections()
		return false, "请求超时，连接已中止"
	case <-done:
		defer transport.CloseIdleConnections()

		if reqErr != nil {
			return false, fmt.Sprintf("SOCKS5代理请求失败: %v", reqErr)
		}
		if resp == nil {
			return false, "响应为空"
		}
		defer resp.Body.Close()

		if !validateContent || responsechecker.IsMediaURL(urlToTest) {
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound {
				return true, fmt.Sprintf("状态码: %d，未验证内容", resp.StatusCode)
			}
			return false, fmt.Sprintf("状态码: %d，未验证内容", resp.StatusCode)
		}

		if resp.StatusCode == http.StatusFound {
			location := resp.Header.Get("Location")
			if location != "" {
				redirectReq, err := http.NewRequestWithContext(ctx, "GET", location, nil)
				if err != nil {
					return false, fmt.Sprintf("构造重定向请求失败: %v", err)
				}
				redirectResp, err := client.Do(redirectReq)
				if err != nil {
					return false, fmt.Sprintf("访问重定向地址失败: %v", err)
				}
				defer redirectResp.Body.Close()
				body, err := io.ReadAll(redirectResp.Body)
				if err != nil {
					return false, fmt.Sprintf("读取重定向响应失败: %v", err)
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
}
