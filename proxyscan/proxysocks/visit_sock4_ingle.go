package proxysocks
import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"github.com/qist/relaycheck/responsechecker"
)
// 单次SOCKS4A请求，返回是否成功、结果信息和重定向地址（无重定向返回空字符串）
func VisitSocks4ASingleRequest(ip string, port int, urlToTest string, uaHeaders map[string][]string, timeout int, validateContent bool, raw bool) (bool, string, string) {
	if strings.HasPrefix(urlToTest, "rtsp://") {
		ok, msg := TestRTSPViaSocks4a(ip, port, urlToTest, timeout)
		return ok, msg, ""
	}

	proxyAddr := fmt.Sprintf("%s:%d", ip, port)
	testURL, err := url.Parse(urlToTest)
	if err != nil {
		return false, fmt.Sprintf("解析URL失败: %v", err), ""
	}

	conn, err := net.DialTimeout("tcp", proxyAddr, time.Duration(timeout)*time.Second)
	if err != nil {
		return false, fmt.Sprintf("SOCKS4A代理连接失败: %v", err), ""
	}
	conn.SetDeadline(time.Now().Add(time.Duration(timeout) * time.Second))

	defer conn.Close()

	targetHost := testURL.Hostname()
	targetPort := 80
	if testURL.Port() != "" {
		targetPort, _ = strconv.Atoi(testURL.Port())
	} else if testURL.Scheme == "https" {
		targetPort = 443
	}

	// SOCKS4A握手
	request := []byte{
		0x04, 0x01,
		byte(targetPort >> 8), byte(targetPort & 0xff),
		0x00, 0x00, 0x00, 0x01, // 0.0.0.1表示使用域名
		0x00,
	}
	request = append(request, []byte(targetHost)...)
	request = append(request, 0x00)
	if _, err := conn.Write(request); err != nil {
		return false, fmt.Sprintf("发送SOCKS4A请求失败: %v", err), ""
	}

	response := make([]byte, 8)
	if _, err := io.ReadFull(conn, response); err != nil {
		return false, fmt.Sprintf("读取SOCKS4A响应失败: %v", err), ""
	}
	if response[1] != 0x5A {
		return false, fmt.Sprintf("SOCKS4A连接失败,响应码: %d", response[1]), ""
	}

	// HTTPS升级
	if testURL.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         targetHost,
		})
		tlsConn.SetDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
		if err := tlsConn.Handshake(); err != nil {
			return false, fmt.Sprintf("TLS握手失败: %v", err), ""
		}
		conn = tlsConn
	}

	req, err := http.NewRequest("GET", urlToTest, nil)
	if err != nil {
		return false, fmt.Sprintf("构造请求失败: %v", err), ""
	}
	req.Host = testURL.Host
	for k, vs := range uaHeaders {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	if err := req.Write(conn); err != nil {
		return false, fmt.Sprintf("SOCKS4A代理请求写入失败: %v", err), ""
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		return false, fmt.Sprintf("解析HTTP响应失败: %v", err), ""
	}
	defer resp.Body.Close()

	// 不验证内容或媒体流时，直接根据状态码判断
	if !validateContent || responsechecker.IsMediaURL(urlToTest) {
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound {
			return true, fmt.Sprintf("状态码: %d，未验证内容", resp.StatusCode), ""
		}
		return false, fmt.Sprintf("状态码: %d，未验证内容", resp.StatusCode), ""
	}

	// 302跳转处理，返回重定向URL，由调用者控制跳转次数
	if resp.StatusCode == http.StatusFound {
		location := resp.Header.Get("Location")
		if location != "" {
			return true, fmt.Sprintf("状态码: 302，重定向到: %s", location), location
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Sprintf("读取响应失败: %v", err), ""
	}

	ok, msg := responsechecker.CheckResponse(resp, body, validateContent, raw)
	return ok, msg, ""
}