package speedtest

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/pion/rtp"
)

type testRTSPServer struct {
	stream *gortsplib.ServerStream
}

func (h *testRTSPServer) OnDescribe(*gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, h.stream, nil
}

func (h *testRTSPServer) OnSetup(*gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return &base.Response{StatusCode: base.StatusOK}, h.stream, nil
}

func (h *testRTSPServer) OnPlay(*gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	return &base.Response{StatusCode: base.StatusOK}, nil
}

// startTestRTSP 启动一个持续推送 RTP 的本地 RTSP 服务，返回 rtsp:// 地址。
func startTestRTSP(t *testing.T) string {
	t.Helper()

	forma := &format.H264{
		PayloadTyp: 96,
		SPS: []byte{
			0x67, 0x42, 0x00, 0x1f, 0x95, 0xa8, 0x28, 0x0f,
			0x64, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00,
			0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc6, 0x58,
		},
		PPS:               []byte{0x68, 0xce, 0x3c, 0x80},
		PacketizationMode: 1,
	}
	media := &description.Media{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{forma},
	}
	desc := &description.Session{Medias: []*description.Media{media}}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("获取测试端口失败: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	rtpPort, rtcpPort := freeUDPPortPair(t)
	handler := &testRTSPServer{}
	server := &gortsplib.Server{
		Handler:        handler,
		RTSPAddress:    addr,
		UDPRTPAddress:  fmt.Sprintf("127.0.0.1:%d", rtpPort),
		UDPRTCPAddress: fmt.Sprintf("127.0.0.1:%d", rtcpPort),
	}
	if err := server.Start(); err != nil {
		t.Fatalf("启动测试 RTSP 服务失败: %v", err)
	}
	stream := &gortsplib.ServerStream{Server: server, Desc: desc}
	if err := stream.Initialize(); err != nil {
		server.Close()
		t.Fatalf("初始化测试流失败: %v", err)
	}
	handler.stream = stream

	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		var seq uint16
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				seq++
				pkt := &rtp.Packet{
					Header: rtp.Header{
						Version:        2,
						PayloadType:    96,
						SequenceNumber: seq,
						Timestamp:      uint32(now.UnixNano() / 1000),
					},
					Payload: bytes.Repeat([]byte{0x41}, 1400),
				}
				// 没有读者时会返回错误，忽略即可，读者接入后自然有数据。
				_ = stream.WritePacketRTP(media, pkt)
			}
		}
	}()

	t.Cleanup(func() {
		close(stop)
		stream.Close()
		server.Close()
	})
	return "rtsp://" + addr + "/stream"
}

// freeUDPPortPair 找一对连续可用的 UDP 端口（RTSP 服务要求 RTP/RTCP 相邻）。
func freeUDPPortPair(t *testing.T) (int, int) {
	t.Helper()
	for i := 0; i < 50; i++ {
		l1, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		p1 := l1.LocalAddr().(*net.UDPAddr).Port
		if p1%2 != 0 {
			l1.Close()
			continue
		}
		l2, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", p1+1))
		l1.Close()
		if err != nil {
			continue
		}
		l2.Close()
		return p1, p1 + 1
	}
	t.Fatal("找不到连续可用 UDP 端口")
	return 0, 0
}

// RTSP 应完成握手并采样 RTP 载荷，经 SOCKS5 也能测出吞吐。
func TestMeasureRTSPThroughSocks5(t *testing.T) {
	targetURL := startTestRTSP(t)
	proxyAddr := startSOCKS5(t)
	host, portStr, _ := net.SplitHostPort(proxyAddr)
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("解析测试代理端口失败: %v", err)
	}

	res := Measure(Options{
		ProxyType: "socks5",
		IP:        host,
		Port:      port,
		URL:       targetURL,
		Timeout:   6 * time.Second,
	})
	if res.Err != nil {
		t.Fatalf("RTSP 测速失败: %v", res.Err)
	}
	if res.Throughput <= 0 {
		t.Fatalf("RTSP 应测出吞吐，got %d (bytes=%d)", res.Throughput, res.Bytes)
	}
}
