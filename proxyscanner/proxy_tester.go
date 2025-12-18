package proxyscanner

import (
	"fmt"
	"github.com/qist/relaycheck/config"
	"github.com/qist/relaycheck/proxyutil"
	"github.com/tidwall/gjson"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// testProxy 代理检测函数
func TestProxy(ip string, port int, successfulIPsCh chan<- string) {
	proxyAddr := fmt.Sprintf("%s:%d", ip, port)
	if !isProxyAlive(ip, port, config.Cfg.ProxyTimeout) {
		log.Printf("失败：代理 %s 无法连接，跳过", proxyAddr)
		return
	}
	retryTimes := config.Cfg.RetryTimes
	if retryTimes <= 0 {
		retryTimes = 1 // 默认至少重试一次
	}
	retryIntervalSeconds := config.Cfg.RetryIntervalSeconds
	if retryIntervalSeconds <= 0 {
		retryIntervalSeconds = 1
	}
	var wg sync.WaitGroup
	resultCh := make(chan struct {
		proxyType string
		ok        bool
		msg       string
		urlToTest string
		elapsed   time.Duration
	}, len(config.Cfg.ProxyTypes)*len(config.Cfg.URLPaths))

	for _, proxyType := range config.Cfg.ProxyTypes {
		start := time.Now() // 记录开始时间
		wg.Add(1)
		go func(pType string) {
			defer wg.Done()
			for _, urlToTest := range config.Cfg.URLPaths {
				ok, msg := proxyutil.ProxyVisit(pType, ip, port, urlToTest,
					config.Cfg.UAHeaders, config.Cfg.HttpProxy, config.Cfg.ProxyTimeout, retryTimes, retryIntervalSeconds)
				elapsed := time.Since(start) // 计算访问耗时
				resultCh <- struct {
					proxyType string
					ok        bool
					msg       string
					urlToTest string
					elapsed   time.Duration
				}{pType, ok, msg, urlToTest, elapsed}
			}
		}(proxyType)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for result := range resultCh {
		if result.ok {
			// 获取本地 IP 所在省份/运营商
			province, isp := getIPInfo(ip)

			// 获取出口 IP 和其归属地
			realIP := getRealIPViaProxy(result.proxyType, ip, port, config.Cfg.RealIPApiURLs, config.Cfg.ProxyTimeout, retryTimes, retryIntervalSeconds)

			rProvince, rISP := getIPInfo(realIP)

			msg := fmt.Sprintf("可用%s代理: %s %s %s 出口IP: %s %s %s 成功访问: %s 耗时: %v\n",
				strings.ToUpper(result.proxyType),
				proxyAddr, province, isp,
				realIP, rProvince, rISP,
				result.urlToTest,
				result.elapsed,
			)

			fmt.Print("成功：", msg)
			successfulIPsCh <- msg
		} else {
			log.Printf("失败：%s代理 %s 访问 %s 失败，%s",
				strings.ToUpper(result.proxyType), proxyAddr, result.urlToTest, result.msg)
		}
	}
}

// 修改连接检测函数，增加读取超时控制
func isProxyAlive(ip string, port int, timeout int) bool {
	// 只检查 TCP 连接是否成功
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ip, port), time.Duration(timeout)*time.Second)
	if err != nil {
		log.Printf("连接失败：%s:%d - %v", ip, port, err)
		return false
	}
	conn.Close()
	return true
}

func getIPInfo(ip string) (province, isp string) {
	client := &http.Client{Timeout: 3 * time.Second}

	for _, apiCfg := range config.Cfg.IPInfoAPIs {
		url := strings.Replace(apiCfg.URL, "{ip}", ip, 1)
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		bodyStr := string(body)
		if gjson.Get(bodyStr, apiCfg.CodeKey).String() != apiCfg.ExpectedCode {
			continue
		}

		province := gjson.Get(bodyStr, apiCfg.ProvinceKey).String()
		isp := gjson.Get(bodyStr, apiCfg.ISPKey).String()
		if province != "" || isp != "" {
			return province, isp
		}
	}

	return "未知", "未知"
}

func getRealIPViaProxy(proxyType, ip string, port int, realIPApiURLs []string, timeout int, retryTimes int, retryIntervalSeconds int) string {
	headers := map[string][]string{"User-Agent": {"curl/7.76.1"}}

	// IPv4 和 IPv6 正则
	reIPv4 := regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reIPv6 := regexp.MustCompile(`(?i)\b([0-9a-f]{1,4}:){1,7}[0-9a-f]{1,4}\b`)

	for _, realIPApiURL := range realIPApiURLs {
		ok, msg := proxyutil.ProxyVisitraw(proxyType, ip, port, realIPApiURL, headers, config.Cfg.HttpProxy, timeout, retryTimes, retryIntervalSeconds)

		// log.Printf("getRealIPViaProxy: proxyType=%s, proxy=%s:%d, url=%s, ok=%v, rawMsg=%q",
		// 	proxyType, ip, port, realIPApiURL, ok, msg)

		if !ok {
			continue
		}

		msg = strings.TrimSpace(msg)

		// 优先匹配 IPv4
		if ip4 := reIPv4.FindString(msg); ip4 != "" {
			// log.Printf("getRealIPViaProxy: 匹配到IPv4: %s", ip4)
			return ip4
		}

		// 再匹配 IPv6（如你愿意使用）
		if ip6 := reIPv6.FindString(msg); ip6 != "" {
			// log.Printf("getRealIPViaProxy: 匹配到IPv6: %s", ip6)
			return ip6
		}
	}

	// log.Printf("getRealIPViaProxy: 所有接口尝试失败或未提取出有效IP，返回未知")
	return "未知"
}
