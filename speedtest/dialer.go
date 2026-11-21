package speedtest

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// contextDialer 与 net/http 的 DialContext、gortsplib 的 DialContext 兼容。
type contextDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// newProxyDialer 按代理类型创建拨号器，供 RTSP 等非 HTTP 场景使用。
func newProxyDialer(opts Options, timeout time.Duration) (contextDialer, error) {
	addr := net.JoinHostPort(opts.IP, strconv.Itoa(opts.Port))
	switch strings.ToLower(strings.TrimSpace(opts.ProxyType)) {
	case "socks5":
		var auth *xproxy.Auth
		if opts.AuthEnabled {
			auth = &xproxy.Auth{User: opts.Username, Password: opts.Password}
		}
		d, err := xproxy.SOCKS5("tcp", addr, auth, &net.Dialer{Timeout: timeout})
		if err != nil {
			return nil, err
		}
		cd, ok := d.(xproxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("SOCKS5 代理不支持上下文拨号")
		}
		return cd, nil
	case "socks4", "socks4a":
		return &socks4aDialer{proxyAddr: addr, timeout: timeout}, nil
	case "http", "https":
		proxyType := strings.ToLower(strings.TrimSpace(opts.ProxyType))
		return &httpConnectDialer{
			proxyAddr:   addr,
			proxyType:   proxyType,
			authEnabled: opts.AuthEnabled,
			username:    opts.Username,
			password:    opts.Password,
			timeout:     timeout,
		}, nil
	default:
		return nil, fmt.Errorf("不支持的代理类型: %s", opts.ProxyType)
	}
}

// httpConnectDialer 通过 HTTP(S) 代理的 CONNECT 建立隧道。
type httpConnectDialer struct {
	proxyAddr   string
	proxyType   string
	authEnabled bool
	username    string
	password    string
	timeout     time.Duration
}

func (d *httpConnectDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{Timeout: d.timeout}).DialContext(ctx, "tcp", d.proxyAddr)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	if d.proxyType == "https" {
		proxyHost, _, _ := net.SplitHostPort(d.proxyAddr)
		tlsConn := tls.Client(conn, &tls.Config{ServerName: proxyHost, InsecureSkipVerify: true})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		conn = tlsConn
	}

	var req strings.Builder
	fmt.Fprintf(&req, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", address, address)
	if d.authEnabled {
		auth := base64.StdEncoding.EncodeToString([]byte(d.username + ":" + d.password))
		fmt.Fprintf(&req, "Proxy-Authorization: Basic %s\r\n", auth)
	}
	req.WriteString("\r\n")
	if _, err := conn.Write([]byte(req.String())); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("HTTP 代理 CONNECT 失败: %s", resp.Status)
	}
	// CONNECT 200 之后 body 就是隧道数据，不能 Close（会关闭底层连接）。
	conn.SetDeadline(time.Time{})
	return &bufferedConn{Conn: conn, r: br}, nil
}

// bufferedConn 保留 bufio 里可能已缓存的隧道数据。
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}
