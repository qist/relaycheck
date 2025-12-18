package proxyscan

import (
	"strings"
	"github.com/qist/relaycheck/config"
)
func ProxyVisitInternal(proxyType, ip string, port int, urlToTest string, uaHeaders map[string][]string, httpProxy *config.HttpProxyConfig, timeout int, validateContent, raw bool, retryTimes int, retryIntervalSeconds int) (bool, string) {
	switch strings.ToLower(proxyType) {
	case "http", "https":
		return VisitHTTPProxy(proxyType, ip, port, urlToTest, uaHeaders, httpProxy, timeout, validateContent, raw, retryTimes, retryIntervalSeconds)
	case "socks5":
		return VisitSocks5Proxy(ip, port, urlToTest, uaHeaders, timeout, validateContent, raw, retryTimes, retryIntervalSeconds)
	case "socks4", "socks4a":
		return VisitSocks4AProxy(ip, port, urlToTest, uaHeaders, timeout, validateContent, raw, retryTimes, retryIntervalSeconds)
	default:
		return false, "不支持的代理类型"
	}
}