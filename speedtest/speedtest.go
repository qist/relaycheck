// Package speedtest 复用 tvgate 的吞吐探测思路：
// 通过代理读取整段正文（直播分片通常 5~6MB），只统计最后 3 秒的速率。
// 前段突发、随后限速或停住的出口不会被全程平均抬高。
package speedtest

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/qist/relaycheck/config"
)

const (
	// DefaultSampleSize 是单次探测最多读取的正文大小。
	// 必须覆盖整个直播分片：只读 2MB 会正好停在「前段突发」区间。
	DefaultSampleSize = 8 << 20
	// DefaultTimeout 是单次测速的默认超时。
	DefaultTimeout = 15 * time.Second
	// probeMinSample 小于该样本量（如 m3u8 播放列表本身）不计算吞吐。
	probeMinSample = 32 << 10
	// probeTailWindow 吞吐只统计最后这段时间的数据，而不是全程平均。
	probeTailWindow = 3 * time.Second
	// probeKeepSample 只保留正文前这么多字节用于类型判断/播放列表解析。
	probeKeepSample = 512 << 10
	// probeMaxRedirects 限制手工跟随的重定向次数。
	probeMaxRedirects = 5
	// probeMaxPlaylistHops 限制 m3u8 → variant → 分片的最大层数。
	probeMaxPlaylistHops = 2
)

// Result 是单次吞吐探测结果。
type Result struct {
	// Throughput 是后段吞吐（字节/秒）。
	// 0 表示读到了数据但后段停住；-1 表示没有有效样本（正文太小等）。
	Throughput int64
	Bytes      int64
	Elapsed    time.Duration
	StatusCode int
	Err        error
}

// Options 描述一次测速所需的代理与目标信息。
type Options struct {
	ProxyType   string
	IP          string
	Port        int
	URL         string
	Headers     map[string][]string
	Timeout     time.Duration
	SampleSize  int
	AuthEnabled bool
	Username    string
	Password    string
}

// Settings 解析配置中的测速参数，未配置时使用默认值并开启测速。
func Settings(c *config.SpeedTestConfig) (enabled bool, timeout time.Duration, sampleSize int) {
	enabled = true
	timeout = DefaultTimeout
	sampleSize = DefaultSampleSize
	if c == nil {
		return enabled, timeout, sampleSize
	}
	enabled = c.Enabled
	if c.Timeout > 0 {
		timeout = time.Duration(c.Timeout) * time.Second
	}
	if c.SampleSizeMB > 0 {
		sampleSize = c.SampleSizeMB << 20
	}
	return enabled, timeout, sampleSize
}

// FormatMbps 把字节/秒格式化为带宽文本；无有效样本或后段停住显示 "-"。
func FormatMbps(bytesPerSec int64) string {
	if bytesPerSec <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1fMbps", Mbps(bytesPerSec))
}

// Mbps 把字节/秒换算为 Mbps。
func Mbps(bytesPerSec int64) float64 {
	if bytesPerSec <= 0 {
		return 0
	}
	return float64(bytesPerSec) * 8 / 1e6
}

// Measure 使用默认后台上下文执行一次测速。
func Measure(opts Options) Result {
	return MeasureContext(context.Background(), opts)
}

// MeasureContext 执行一次测速：
//   - http(s) 目标：经代理发 Range 请求，读满采样上限后取后段速率；
//   - 目标为 m3u8：继续取首个 variant/最后一个分片，避免播放列表太小测不出带宽。
func MeasureContext(ctx context.Context, opts Options) Result {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	size := opts.SampleSize
	if size <= 0 {
		size = DefaultSampleSize
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if lower := strings.ToLower(opts.URL); strings.HasPrefix(lower, "rtsp://") || strings.HasPrefix(lower, "rtsps://") {
		return measureRTSP(ctx, opts, timeout)
	}

	client, closeFn, err := newClient(opts, timeout)
	if err != nil {
		return Result{Throughput: -1, Err: err}
	}
	defer closeFn()

	return measure(ctx, client, opts.URL, opts.Headers, size)
}

func newClient(opts Options, timeout time.Duration) (*http.Client, func(), error) {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: 2 * time.Second,
		IdleConnTimeout:       10 * time.Second,
		ForceAttemptHTTP2:     false,
	}
	closeFn := transport.CloseIdleConnections

	addr := net.JoinHostPort(opts.IP, strconv.Itoa(opts.Port))
	proxyType := strings.ToLower(strings.TrimSpace(opts.ProxyType))
	switch proxyType {
	case "socks5", "http", "https":
		proxyURL := &url.URL{Scheme: proxyType, Host: addr}
		if opts.AuthEnabled {
			proxyURL.User = url.UserPassword(opts.Username, opts.Password)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks4", "socks4a":
		transport.DialContext = (&socks4aDialer{proxyAddr: addr, timeout: timeout}).DialContext
	default:
		transport.CloseIdleConnections()
		return nil, nil, fmt.Errorf("不支持的代理类型: %s", opts.ProxyType)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		// 手工跟随重定向，确保每一跳都带 Range。
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client, closeFn, nil
}

func measure(ctx context.Context, client *http.Client, targetURL string, headers map[string][]string, size int) Result {
	out := Result{Throughput: -1}

	resp, err := probeRequest(ctx, client, targetURL, headers, size)
	if err != nil {
		out.Err = err
		return out
	}
	defer resp.Body.Close()

	out.StatusCode = resp.StatusCode
	if resp.StatusCode >= 400 {
		out.Err = fmt.Errorf("状态码: %d", resp.StatusCode)
		return out
	}

	sample, total, elapsed, tp := readSample(resp.Body, size)
	out.Bytes, out.Elapsed, out.Throughput = total, elapsed, tp
	if !isPlaylistSample(resp.Header.Get("Content-Type"), sample) {
		return out
	}

	// m3u8 正文通常只有几百字节，读不满采样上限；继续取首个
	// variant/分片做一次采样，否则吞吐永远测不出来。
	base := responseURL(resp)
	next := playlistSampleURI(sample, base)
	for hop := 0; hop < probeMaxPlaylistHops && next != ""; hop++ {
		segResp, segErr := probeRequest(ctx, client, next, headers, size)
		if segErr != nil {
			// 播放列表本身已探测成功，子请求失败不把节点判死。
			break
		}
		segSample, segTotal, segElapsed, segTP := readSample(segResp.Body, size)
		segType := segResp.Header.Get("Content-Type")
		segBase := responseURL(segResp)
		segStatus := segResp.StatusCode
		segResp.Body.Close()
		if segStatus >= 400 {
			break
		}
		if isPlaylistSample(segType, segSample) {
			next = playlistSampleURI(segSample, segBase)
			continue
		}
		if segTP > out.Throughput {
			out.Bytes, out.Elapsed, out.Throughput = segTotal, segElapsed, segTP
		}
		break
	}
	return out
}

// probeRequest 手工跟随重定向，避免只看到 302 而拿不到正文。
func probeRequest(ctx context.Context, client *http.Client, targetURL string, headers map[string][]string, size int) (*http.Response, error) {
	reqURL := targetURL
	for i := 0; i <= probeMaxRedirects; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}
		for k, vs := range headers {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", size-1))

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if !isRedirectStatus(resp.StatusCode) {
			return resp, nil
		}
		loc := resp.Header.Get("Location")
		if loc == "" {
			return resp, nil
		}
		base := responseURL(resp)
		if base == nil {
			base, _ = url.Parse(reqURL)
		}
		var next *url.URL
		if base != nil {
			next, err = base.Parse(loc)
		} else {
			next, err = url.Parse(loc)
		}
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		reqURL = next.String()
	}
	return nil, fmt.Errorf("测速重定向次数超过 %d", probeMaxRedirects)
}

func responseURL(resp *http.Response) *url.URL {
	if resp != nil && resp.Request != nil {
		return resp.Request.URL
	}
	return nil
}

func isRedirectStatus(code int) bool {
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

// probeMark 记录某个时刻的累计读取字节，用于计算「后段」速率。
type probeMark struct {
	at time.Duration
	n  int64
}

// readSample 读取探测正文，返回保留的样本、总字节、耗时与后段吞吐。
func readSample(r io.Reader, size int) ([]byte, int64, time.Duration, int64) {
	if size <= 0 {
		size = DefaultSampleSize
	}
	start := time.Now()
	limit := int64(size)
	keep := int64(probeKeepSample)
	buf := make([]byte, 32<<10)
	sample := make([]byte, 0, 64<<10)
	marks := make([]probeMark, 0, 64)
	var total int64
	for total < limit {
		n, err := r.Read(buf)
		if n > 0 {
			if remain := limit - total; int64(n) > remain {
				n = int(remain)
			}
			if int64(len(sample)) < keep {
				take := n
				if int64(take) > keep-int64(len(sample)) {
					take = int(keep - int64(len(sample)))
				}
				sample = append(sample, buf[:take]...)
			}
			total += int64(n)
			marks = append(marks, probeMark{at: time.Since(start), n: total})
		}
		if err != nil {
			break
		}
	}
	elapsed := time.Since(start)
	return sample, total, elapsed, tailThroughput(total, elapsed, marks)
}

// tailThroughput 只统计最后 probeTailWindow 的速率。
// 总耗时不足一个窗口（高速出口很快读完）时退回全程平均。
// 返回 -1 表示没有有效样本；返回 0 表示读到了数据、但后段没有字节。
func tailThroughput(total int64, elapsed time.Duration, marks []probeMark) int64 {
	if total < probeMinSample || elapsed <= 0 {
		return -1
	}
	if elapsed <= probeTailWindow {
		return int64(float64(total) / elapsed.Seconds())
	}
	cutoff := elapsed - probeTailWindow
	var base int64
	for _, m := range marks {
		if m.at <= cutoff {
			base = m.n
		} else {
			break
		}
	}
	tail := total - base
	if tail <= 0 {
		return 0
	}
	return int64(float64(tail) / probeTailWindow.Seconds())
}

func isPlaylistSample(contentType string, sample []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "mpegurl") {
		return true
	}
	head := sample
	if len(head) > 64 {
		head = head[:64]
	}
	head = bytes.TrimLeft(head, " \t\r\n")
	return bytes.HasPrefix(head, []byte("#EXTM3U"))
}

// playlistSampleURI 从 m3u8 里挑一个可用于吞吐采样的地址：
// master 取第一个 variant，媒体播放列表取最后一个分片。
func playlistSampleURI(sample []byte, base *url.URL) string {
	master := bytes.Contains(sample, []byte("#EXT-X-STREAM-INF")) ||
		bytes.Contains(sample, []byte("#EXT-X-I-FRAME-STREAM-INF"))
	var first, last string
	for _, line := range strings.Split(string(sample), "\n") {
		lt := strings.TrimSpace(line)
		if lt == "" || strings.HasPrefix(lt, "#") {
			continue
		}
		ref, err := url.Parse(lt)
		if err != nil {
			continue
		}
		u := ref.String()
		if base != nil {
			u = base.ResolveReference(ref).String()
		}
		if first == "" {
			first = u
		}
		last = u
	}
	if master {
		return first
	}
	return last
}

// socks4aDialer 为 SOCKS4A 代理提供 DialContext（net/http 只原生支持 socks5）。
type socks4aDialer struct {
	proxyAddr string
	timeout   time.Duration
}

func (d *socks4aDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}

	conn, err := (&net.Dialer{Timeout: d.timeout}).DialContext(ctx, "tcp", d.proxyAddr)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	request := []byte{0x04, 0x01, byte(port >> 8), byte(port), 0x00, 0x00, 0x00, 0x01, 0x00}
	request = append(request, []byte(host)...)
	request = append(request, 0x00)
	if _, err := conn.Write(request); err != nil {
		conn.Close()
		return nil, err
	}

	response := make([]byte, 8)
	if _, err := io.ReadFull(conn, response); err != nil {
		conn.Close()
		return nil, err
	}
	if response[1] != 0x5A {
		conn.Close()
		return nil, fmt.Errorf("SOCKS4A 连接失败, 响应码: %d", response[1])
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}
