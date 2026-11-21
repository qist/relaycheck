package proxyscan
import (
	"fmt"
	"time"
	"github.com/qist/relaycheck/proxyscan/proxysocks"
)

func VisitSocks5Proxy(ip string, port int, urlToTest string, uaHeaders map[string][]string, timeout int, validateContent bool, raw bool, retryTimes int, retryIntervalSeconds int) (bool, string) {
	lastMsg := "未知错误"
	for attempt := 1; attempt <= retryTimes; attempt++ {
		ok, msg := proxysocks.VisitSocks5SingleRequest(ip, port, urlToTest, uaHeaders, timeout, validateContent, raw)
		if ok {
			return true, msg
		}
		lastMsg = msg

		// 重试前等待 timeout 秒（除最后一次）
		if attempt < retryTimes && retryIntervalSeconds > 0 {
			time.Sleep(time.Duration(retryIntervalSeconds) * time.Second)
		}
	}
	if retryTimes <= 1 {
		return false, lastMsg
	}
	return false, fmt.Sprintf("尝试 %d 次后仍失败: %s", retryTimes, lastMsg)
}
