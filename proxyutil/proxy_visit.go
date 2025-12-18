package proxyutil
import (
	"github.com/qist/relaycheck/config"
	"github.com/qist/relaycheck/proxyscan"
)
// 统一代理访问接口
func ProxyVisit(proxyType, ip string, port int, urlToTest string, uaHeaders map[string][]string, httpProxy *config.HttpProxyConfig, timeout int, retryTimes int, retryIntervalSeconds int) (bool, string) {
	return proxyscan.ProxyVisitInternal(proxyType, ip, port, urlToTest, uaHeaders, httpProxy, timeout, config.Cfg.ValidateContent, false, retryTimes, retryIntervalSeconds)
}
