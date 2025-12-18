package proxyscan
import (
	"fmt"
	"time"
	"github.com/qist/relaycheck/proxyscan/proxysocks"
)
// SOCKS4/SOCKS4A代理

func VisitSocks4AProxy(ip string, port int, urlToTest string, uaHeaders map[string][]string, timeout int, validateContent bool, raw bool, retryTimes int, retryIntervalSeconds int) (bool, string) {
	const maxRedirects = 5

	for attempt := 1; attempt <= retryTimes; attempt++ {
		currentURL := urlToTest

		for redirectCount := 0; redirectCount <= maxRedirects; redirectCount++ {
			ok, result, redirectLocation := proxysocks.VisitSocks4ASingleRequest(ip, port, currentURL, uaHeaders, timeout, validateContent, raw)
			if !ok {
				// 这次失败，但还有重试机会
				break
			}
			if redirectLocation == "" {
				// 成功且无重定向
				return true, result
			}
			// 继续重定向
			currentURL = redirectLocation
		}

		// 当前 attempt 的所有重定向都失败了，等待后重试
		if attempt < retryTimes && retryIntervalSeconds > 0 {
			time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
		}
	}

	return false, fmt.Sprintf("尝试 %d 次后仍失败", retryTimes)
}