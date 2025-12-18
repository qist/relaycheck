package proxyscan
import (
	"fmt"
	"time"
	"github.com/qist/relaycheck/config"
	"github.com/qist/relaycheck/proxyscan/proxyhttp"
)
// visitHTTPProxy 尝试通过 HTTP/HTTPS 代理访问目标 URL，支持自定义 CONNECT 头部，处理跳转，返回是否成功与说明
func VisitHTTPProxy(proxyType, ip string, port int, targetURL string, uaHeaders map[string][]string, httpProxy *config.HttpProxyConfig, timeoutSeconds int, validateContent bool, raw bool, retryTimes int, retryIntervalSeconds int) (bool, string) {
	for attempt := 1; attempt <= retryTimes; attempt++ {
		ok, msg := proxyhttp.VisitHTTPSingleRequest(proxyType, ip, port, targetURL, uaHeaders, httpProxy, timeoutSeconds, validateContent, raw)
		if ok {
			return true, msg
		}
		if attempt < retryTimes && retryIntervalSeconds > 0 {
			time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
		}
	}
	return false, fmt.Sprintf("尝试 %d 次后仍失败", retryTimes)
}