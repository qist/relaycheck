package tvgate

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/qist/relaycheck/config"
)

// GenerateTVGateYAML 从日志生成 TVGate YAML
func GenerateTVGateYAML(inputFileName, outputFileName, groupName string, maxElapsedSec float64, cfg *config.Config) error {
	if inputFileName == "" {
		inputFileName = cfg.SuccessfulIPsFile
	}
	if outputFileName == "" {
		outputFileName = "filtered_proxies.yaml"
	}
	if groupName == "" {
		groupName = "shuxiaoguo"
	}

	inputFile, err := os.Open(inputFileName)
	if err != nil {
		return fmt.Errorf("打开输入文件失败: %v", err)
	}
	defer inputFile.Close()

	outputFile, err := os.Create(outputFileName)
	if err != nil {
		return fmt.Errorf("创建输出文件失败: %v", err)
	}
	defer outputFile.Close()

	scanner := bufio.NewScanner(inputFile)
	counter := 1

	ipPortRe := regexp.MustCompile(`可用(\w+)代理:\s*([0-9.]+):(\d+)`)
	timeRe := regexp.MustCompile(`耗时:\s*([0-9.]+)(ms|s)`)

	// 写 YAML 顶部
	outputFile.WriteString("proxygroups:\n")
	outputFile.WriteString(fmt.Sprintf("  %s:\n", groupName))
	outputFile.WriteString("    proxies:\n")

	for scanner.Scan() {
		line := scanner.Text()
		ipPortMatch := ipPortRe.FindStringSubmatch(line)
		timeMatch := timeRe.FindStringSubmatch(line)
		if ipPortMatch == nil || timeMatch == nil {
			continue
		}

		proxyType := strings.ToLower(ipPortMatch[1])
		ip := ipPortMatch[2]
		port, _ := strconv.Atoi(ipPortMatch[3])

		// 解析耗时
		elapsed := 0.0
		if timeMatch[2] == "ms" {
			elapsed, _ = strconv.ParseFloat(timeMatch[1], 64)
			elapsed = elapsed / 1000
		} else {
			elapsed, _ = strconv.ParseFloat(timeMatch[1], 64)
		}

		// 耗时过滤
		if maxElapsedSec > 0 && elapsed >= maxElapsedSec {
			continue
		}

		yamlEntry := fmt.Sprintf("      - name: \"%s%d\"\n        type: %s\n        server: %s\n        port: %d\n",
			groupName, counter, proxyType, ip, port)

		// 代理认证
		if cfg.ProxyAuthEnabled && cfg.ProxyUsername != "" && cfg.ProxyPassword != "" {
			yamlEntry += fmt.Sprintf("        username: %s\n        password: %s\n", cfg.ProxyUsername, cfg.ProxyPassword)
		}

		// SOCKS 系列才加 udp: true
		if proxyType == "socks5" || proxyType == "socks4" || proxyType == "socks4a" {
			yamlEntry += "        udp: true\n"
		}

		outputFile.WriteString(yamlEntry)
		counter++
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取文件错误: %v", err)
	}

	fmt.Printf("筛选完成，已输出到 %s\n", outputFileName)
	return nil
}
