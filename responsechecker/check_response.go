package responsechecker
import (
	"fmt"
	"net/http"
	"strings"
)
// 添加一个检查响应内容的通用函数
func CheckResponse(resp *http.Response, body []byte, validateContent, raw bool) (bool, string) {
	if raw {
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound {
			return true, string(body)
		}
		return false, fmt.Sprintf("状态码非200/302: %d", resp.StatusCode)
	}

	content := string(body)
	hasContent := !validateContent || isPlayableM3U8Content(content)

	statusMsg := fmt.Sprintf("状态码: %d", resp.StatusCode)
	contentMsg := ""
	if hasContent {
		contentMsg = "，内容匹配"
	} else {
		contentMsg = "，内容不匹配"
	}
	msg := statusMsg + contentMsg

	if (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusFound) && hasContent {
		return true, msg
	}
	return false, msg
}

func isPlayableM3U8Content(content string) bool {
	containsVersion := strings.Contains(content, "EXT-X-VERSION")
	containsStream := strings.Contains(content, "EXT-X-STREAM-INF")
	containsDefaultVhost := strings.Contains(content, "_defaultVhost_")
	containsSegments := strings.Contains(content, "EXT-X-INDEPENDENT-SEGMENTS")
	containsExtInf := strings.Contains(content, "EXTINF")
	containsHttp := strings.Contains(content, "http://")
	containsMk := strings.Contains(content, `"Ret":20102,"Reason":"`)

	// 不可播放情况（结构性骨架）
	if (containsVersion && containsStream && !containsSegments) ||
		(containsStream && containsDefaultVhost) ||
		(containsStream && containsHttp) {
		return false
	}

	// 可播放情况
	if (containsVersion && containsExtInf) ||
		containsStream ||
		containsMk ||
		(containsVersion && containsSegments) {
		return true
	}

	return false
}

// SOCKS5代理
func IsMediaURL(u string) bool {
	u = strings.ToLower(u)
	return strings.HasSuffix(u, ".flv") ||
		strings.HasSuffix(u, ".ts") ||
		strings.HasSuffix(u, ".mp4")
}