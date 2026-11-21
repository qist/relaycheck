package speedtest

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/pion/rtp"
)

const (
	// rtspSampleWindow 是 RTSP 握手成功后继续接收 RTP 的采样时长。
	// RTSP 是实时流，2s 通常覆盖一个 GOP，能反映真实码率/出口带宽。
	rtspSampleWindow = 2 * time.Second
	// rtspMinSampleBytes 小于该样本量时不给吞吐，避免首包关键帧造成虚高。
	rtspMinSampleBytes = 32 << 10
	// rtspSampleGuard 是 ctx 截止前留给关闭连接的余量。
	rtspSampleGuard = 200 * time.Millisecond
)

// measureRTSP 通过代理完成 OPTIONS + DESCRIBE + SETUP + PLAY，
// PLAY 后采样 RTP 载荷统计吞吐。与 tvgate 的 RTSP 测速一致。
func measureRTSP(ctx context.Context, opts Options, timeout time.Duration) Result {
	out := Result{Throughput: -1}

	dialer, err := newProxyDialer(opts, timeout)
	if err != nil {
		out.Err = err
		return out
	}

	parsedURL, err := base.ParseURL(opts.URL)
	if err != nil {
		out.Err = fmt.Errorf("解析RTSP地址失败: %w", err)
		return out
	}
	host := parsedURL.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(parsedURL.Hostname(), "554")
	}

	transport := gortsplib.TransportTCP
	client := &gortsplib.Client{
		Transport:    &transport,
		TLSConfig:    &tls.Config{InsecureSkipVerify: true},
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		UserAgent:    headerValue(opts.Headers, "User-Agent"),
		DialContext: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(dialCtx, network, addr)
		},
	}
	defer client.Close()

	start := time.Now()
	if err := client.Start(parsedURL.Scheme, host); err != nil {
		out.Err = fmt.Errorf("RTSP握手失败: %w", err)
		return out
	}
	if _, err := client.Options(parsedURL); err != nil {
		out.Err = fmt.Errorf("OPTIONS失败: %w", err)
		return out
	}
	session, _, err := client.Describe(parsedURL)
	if err != nil {
		out.Err = fmt.Errorf("DESCRIBE失败: %w", err)
		return out
	}
	if !hasSupportedVideo(session) {
		out.Err = fmt.Errorf("未发现支持的视频流（无 H264/H265/VP8/VP9/MPEGTS）")
		return out
	}
	if err := client.SetupAll(session.BaseURL, session.Medias); err != nil {
		out.Err = fmt.Errorf("SETUP失败: %w", err)
		return out
	}

	var recvBytes atomic.Int64
	client.OnPacketRTPAny(func(_ *description.Media, _ format.Format, pkt *rtp.Packet) {
		recvBytes.Add(int64(len(pkt.Payload)))
	})

	if _, err := client.Play(nil); err != nil {
		out.Err = fmt.Errorf("PLAY失败: %w", err)
		return out
	}
	out.Elapsed = time.Since(start)

	window := rtspSampleBudget(ctx)
	if window <= 0 {
		return out
	}
	sampleStart := time.Now()
	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
	elapsed := time.Since(sampleStart)

	got := recvBytes.Load()
	out.Bytes = got
	if got >= rtspMinSampleBytes && elapsed > 0 {
		out.Throughput = int64(float64(got) / elapsed.Seconds())
	}
	return out
}

// rtspSampleBudget 计算采样窗口：最多 rtspSampleWindow，且不能吃掉 ctx 余量。
func rtspSampleBudget(ctx context.Context) time.Duration {
	window := rtspSampleWindow
	if deadline, ok := ctx.Deadline(); ok {
		if remain := time.Until(deadline) - rtspSampleGuard; remain < window {
			window = remain
		}
	}
	if window < 500*time.Millisecond {
		return 0
	}
	return window
}

func hasSupportedVideo(session *description.Session) bool {
	for _, media := range session.Medias {
		for _, forma := range media.Formats {
			switch forma.(type) {
			case *format.H264, *format.H265, *format.VP8, *format.VP9, *format.MPEGTS:
				return true
			}
		}
	}
	return false
}

// headerValue 按大小写不敏感的方式取第一个请求头值。
func headerValue(headers map[string][]string, name string) string {
	for k, vs := range headers {
		if !strings.EqualFold(k, name) || len(vs) == 0 {
			continue
		}
		return vs[0]
	}
	return ""
}
