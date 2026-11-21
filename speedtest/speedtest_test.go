package speedtest

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qist/relaycheck/config"
)

func buildMarks(bytesPerSec int64, seconds int, step time.Duration) (int64, []probeMark) {
	var total int64
	marks := make([]probeMark, 0, int(float64(seconds)/step.Seconds())+1)
	for t := time.Duration(0); t <= time.Duration(seconds)*time.Second; t += step {
		total += int64(float64(bytesPerSec) * step.Seconds())
		marks = append(marks, probeMark{at: t, n: total})
	}
	return total, marks
}

// 前段突发、随后停住：后段窗口应记 0，而不是被全程平均抬高。
func TestTailThroughputIgnoresBurst(t *testing.T) {
	total, marks := buildMarks(500_000, 6, 500*time.Millisecond)
	if got := tailThroughput(total, 10*time.Second, marks); got != 0 {
		t.Fatalf("突然后停住应记 0 吞吐，got %d", got)
	}
}

// 稳定节点不应被后段窗口误伤。
func TestTailThroughputStableNode(t *testing.T) {
	total, marks := buildMarks(500_000, 10, time.Second) // 4Mbps
	got := tailThroughput(total, 10*time.Second, marks)
	want := int64(500_000)
	if got < want*8/10 || got > want*12/10 {
		t.Fatalf("稳定节点后段吞吐应接近 %d，got %d", want, got)
	}
}

// 高速出口不足一个窗口就 EOF 时，退回全程平均。
func TestTailThroughputShortSampleUsesAverage(t *testing.T) {
	total, marks := buildMarks(2_000_000, 1, 100*time.Millisecond)
	if got := tailThroughput(total, time.Second, marks); got <= 0 {
		t.Fatalf("短样本应回退全程平均，got %d", got)
	}
}

// 播放列表等小正文不算吞吐。
func TestTailThroughputIgnoresTinySample(t *testing.T) {
	total, marks := buildMarks(10_000, 1, 100*time.Millisecond)
	if got := tailThroughput(total, time.Second, marks); got != -1 {
		t.Fatalf("小正文应返回无样本 -1，got %d", got)
	}
}

func TestFormatMbps(t *testing.T) {
	if got := FormatMbps(0); got != "-" {
		t.Fatalf("0 吞吐应显示 -，got %q", got)
	}
	if got := FormatMbps(1_250_000); got != "10.0Mbps" {
		t.Fatalf("1.25MB/s 应显示 10.0Mbps，got %q", got)
	}
}

// 302 → m3u8 → 分片的完整链路：最终应测出分片吞吐而不是播放列表的 0。
func TestMeasureFollowsRedirectAndPlaylist(t *testing.T) {
	segment := bytes.Repeat([]byte{0x47}, 4<<20)
	mux := http.NewServeMux()
	mux.HandleFunc("/index.m3u8", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/real.m3u8?token=1", http.StatusFound)
	})
	mux.HandleFunc("/real.m3u8", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\nseg.ts\n")
	})
	mux.HandleFunc("/seg.ts", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "seg.ts", time.Time{}, bytes.NewReader(segment))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	got := measure(t.Context(), client, server.URL+"/index.m3u8", nil, 4<<20)
	if got.Err != nil {
		t.Fatalf("测速失败: %v", got.Err)
	}
	if got.Throughput <= 0 {
		t.Fatalf("分片吞吐应大于 0，got %d", got.Throughput)
	}
}

// 经本地 SOCKS5 代理测速，验证代理链路与吞吐计算可用。
func TestMeasureThroughSocks5(t *testing.T) {
	segment := bytes.Repeat([]byte{0x47}, 4<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "seg.ts", time.Time{}, bytes.NewReader(segment))
	}))
	defer server.Close()

	proxyAddr := startSOCKS5(t)
	host, portStr, _ := net.SplitHostPort(proxyAddr)
	port, _ := strconv.Atoi(portStr)

	res := Measure(Options{
		ProxyType:  "socks5",
		IP:         host,
		Port:       port,
		URL:        server.URL,
		Timeout:    5 * time.Second,
		SampleSize: 4 << 20,
	})
	if res.Err != nil {
		t.Fatalf("经 SOCKS5 测速失败: %v", res.Err)
	}
	if res.Throughput <= 0 {
		t.Fatalf("经 SOCKS5 应测出吞吐，got %d", res.Throughput)
	}
}

// RunFile 应解析裸 ip:port、测速并按吞吐写入结果文件。
func TestRunFileWritesThroughput(t *testing.T) {
	segment := bytes.Repeat([]byte{0x47}, 4<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "seg.ts", time.Time{}, bytes.NewReader(segment))
	}))
	defer server.Close()

	proxyAddr := startSOCKS5(t)
	dir := t.TempDir()
	input := filepath.Join(dir, "proxies.txt")
	if err := os.WriteFile(input, []byte(proxyAddr+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "result.txt")
	cfg := &config.Config{
		URLPaths:   []string{server.URL},
		ProxyTypes: []string{"socks5"},
		SpeedTest:  &config.SpeedTestConfig{Enabled: true, Timeout: 5, SampleSizeMB: 4},
	}
	if err := RunFile(input, output, "", cfg, nil); err != nil {
		t.Fatalf("RunFile 失败: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "\tSOCKS5\t"+proxyAddr+"\t") {
		t.Fatalf("结果文件缺少代理行: %s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, proxyAddr) && strings.HasPrefix(line, "0.00\t") {
			t.Fatalf("本地代理测速不应为 0: %s", text)
		}
	}
}

func startSOCKS5(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动测试 SOCKS5 失败: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSOCKS5(conn)
		}
	}()
	return ln.Addr().String()
}

func serveSOCKS5(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)

	head := make([]byte, 2)
	if _, err := io.ReadFull(br, head); err != nil {
		return
	}
	if _, err := io.ReadFull(br, make([]byte, int(head[1]))); err != nil {
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	req := make([]byte, 4)
	if _, err := io.ReadFull(br, req); err != nil {
		return
	}
	var host string
	switch req[3] {
	case 0x01:
		b := make([]byte, 4)
		if _, err := io.ReadFull(br, b); err != nil {
			return
		}
		host = net.IP(b).String()
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(br, l); err != nil {
			return
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(br, b); err != nil {
			return
		}
		host = string(b)
	case 0x04:
		b := make([]byte, 16)
		if _, err := io.ReadFull(br, b); err != nil {
			return
		}
		host = net.IP(b).String()
	default:
		return
	}
	p := make([]byte, 2)
	if _, err := io.ReadFull(br, p); err != nil {
		return
	}
	port := int(p[0])<<8 | int(p[1])

	target, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 3*time.Second)
	if err != nil {
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer target.Close()
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	go io.Copy(target, br)
	io.Copy(conn, target)
}

func TestParseProxyLine(t *testing.T) {
	e, ok := parseProxyLine("可用SOCKS5代理: 117.186.239.122:16849 上海市 移动 出口IP: 58.33.102.52 成功访问: rtsp://1.2.3.4/live 吞吐: 8.0Mbps 耗时: 1s", "http")
	if !ok || e.proxyType != "SOCKS5" || e.ip != "117.186.239.122" || e.port != 16849 {
		t.Fatalf("解析成功日志失败: %+v ok=%v", e, ok)
	}
	if e.url != "rtsp://1.2.3.4/live" {
		t.Fatalf("应解析出成功访问的 RTSP 目标，got %q", e.url)
	}
	e, ok = parseProxyLine("1.2.3.4:8080", "http")
	if !ok || e.proxyType != "http" || e.addr != "1.2.3.4:8080" {
		t.Fatalf("解析裸 ip:port 失败: %+v ok=%v", e, ok)
	}
	if _, ok := parseProxyLine("not a proxy", "http"); ok {
		t.Fatal("非法行不应被解析")
	}
}
