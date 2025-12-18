
package proxyutil
import (
	"github.com/qist/relaycheck/config"
	"github.com/qist/relaycheck/proxyscan"
)

func ProxyVisitraw(proxyType, ip string, port int, urlToTest string, uaHeaders map[string][]string, httpProxy *config.HttpProxyConfig, timeout int, retryTimes int, retryIntervalSeconds int) (bool, string) {
	return proxyscan.ProxyVisitInternal(proxyType, ip, port, urlToTest, uaHeaders, httpProxy, timeout, true, true, retryTimes, retryIntervalSeconds)
}