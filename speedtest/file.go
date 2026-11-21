package speedtest

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/qist/relaycheck/config"
)

const defaultSpeedResultFile = "speed_result.txt"

// entry 是测速输入中的一个代理。
type entry struct {
	proxyType string
	ip        string
	port      int
	addr      string
	url       string // 成功日志里记录的访问目标（可为空）
}

// measured 是写入结果文件的一行。
type measured struct {
	entry
	target string
	result Result
}

// RunFile 读取代理列表（支持 relaycheck 成功日志或每行 ip:port），
// 逐个测速后按吞吐从高到低写入 outputFile。
func RunFile(inputFile, outputFile, targetURL string, cfg *config.Config, progress func(string)) error {
	if cfg == nil {
		return fmt.Errorf("配置为空")
	}
	if inputFile == "" {
		return fmt.Errorf("未指定输入文件")
	}
	explicitTarget := targetURL != ""
	if !explicitTarget && len(cfg.URLPaths) > 0 {
		targetURL = cfg.URLPaths[0]
	}
	if outputFile == "" {
		outputFile = defaultSpeedResultFile
	}

	entries, err := loadEntries(inputFile, cfg.ProxyTypes)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("输入文件 %s 中没有可测速的代理", inputFile)
	}
	if targetURL == "" {
		for _, e := range entries {
			if e.url != "" {
				targetURL = e.url
				break
			}
		}
	}
	if targetURL == "" {
		return fmt.Errorf("未配置测速目标 URL")
	}

	_, timeout, sampleSize := Settings(cfg.SpeedTest)
	report := func(format string, args ...interface{}) {
		if progress != nil {
			progress(fmt.Sprintf(format, args...))
		}
	}

	results := make([]measured, len(entries))
	concurrency := 8
	if cfg.SpeedTest != nil && cfg.SpeedTest.Concurrency > 0 {
		concurrency = cfg.SpeedTest.Concurrency
	}
	if concurrency > 128 {
		concurrency = 128
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i := range entries {
		e := entries[i]
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, e entry) {
			defer wg.Done()
			defer func() { <-sem }()
			useURL := targetURL
			if !explicitTarget && e.url != "" {
				useURL = e.url
			}
			res := Measure(Options{
				ProxyType:   e.proxyType,
				IP:          e.ip,
				Port:        e.port,
				URL:         useURL,
				Headers:     cfg.UAHeaders,
				Timeout:     timeout,
				SampleSize:  sampleSize,
				AuthEnabled: cfg.ProxyAuthEnabled,
				Username:    cfg.ProxyUsername,
				Password:    cfg.ProxyPassword,
			})
			results[idx] = measured{entry: e, target: useURL, result: res}
			report("测速完成 %-22s 吞吐: %s\n", e.addr, FormatMbps(res.Throughput))
		}(i, e)
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool {
		ti, tj := results[i].result.Throughput, results[j].result.Throughput
		if ti != tj {
			return ti > tj
		}
		return results[i].addr < results[j].addr
	})

	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("创建结果文件失败: %v", err)
	}
	defer file.Close()

	w := bufio.NewWriter(file)
	fmt.Fprintln(w, "# 吞吐(Mbps)\t类型\t代理\t目标\t备注")
	failures := 0
	for _, item := range results {
		note := resultNote(item.result)
		if item.result.Throughput <= 0 {
			failures++
		}
		fmt.Fprintf(w, "%.2f\t%s\t%s\t%s\t%s\n",
			Mbps(item.result.Throughput), strings.ToUpper(item.proxyType), item.addr, item.target, note)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("写入结果文件失败: %v", err)
	}

	fmt.Printf("测速完成，共 %d 个代理，%d 个可用吞吐样本，结果已写入 %s\n",
		len(results), len(results)-failures, outputFile)
	return nil
}

func resultNote(res Result) string {
	if res.Err != nil {
		return strings.Join(strings.Fields(res.Err.Error()), " ")
	}
	if res.Throughput == 0 {
		return "后段停住"
	}
	if res.Throughput < 0 {
		return "无有效样本"
	}
	return ""
}

// loadEntries 去重解析代理列表。
// 支持：可用SOCKS5代理: 1.2.3.4:8080 ...；也支持裸 ip:port（用配置的第一个代理类型）。
func loadEntries(filename string, defaultTypes []string) ([]entry, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("打开输入文件失败: %v", err)
	}
	defer file.Close()

	defaultType := "socks5"
	if len(defaultTypes) > 0 && strings.TrimSpace(defaultTypes[0]) != "" {
		defaultType = strings.TrimSpace(defaultTypes[0])
	}

	seen := make(map[string]bool)
	var entries []entry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		e, ok := parseProxyLine(scanner.Text(), defaultType)
		if !ok {
			continue
		}
		key := strings.ToLower(e.proxyType) + "@" + e.addr
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取输入文件失败: %v", err)
	}
	return entries, nil
}

func parseProxyLine(line, defaultType string) (entry, bool) {
	line = strings.TrimSpace(line)
	raw := line
	proxyType := defaultType

	if i := strings.Index(line, "代理:"); i >= 0 {
		proxyType = strings.TrimSpace(strings.TrimPrefix(line[:i], "可用"))
		rest := strings.TrimSpace(line[i+len("代理:"):])
		if fields := strings.Fields(rest); len(fields) > 0 {
			line = fields[0]
		}
	} else if i := strings.Index(line, "代理："); i >= 0 {
		proxyType = strings.TrimSpace(strings.TrimPrefix(line[:i], "可用"))
		rest := strings.TrimSpace(line[i+len("代理："):])
		if fields := strings.Fields(rest); len(fields) > 0 {
			line = fields[0]
		}
	}
	if proxyType == "" {
		proxyType = defaultType
	}

	idx := strings.LastIndex(line, ":")
	if idx <= 0 {
		return entry{}, false
	}
	host := strings.TrimSpace(strings.Trim(line[:idx], "[]"))
	port, err := strconv.Atoi(strings.TrimSpace(line[idx+1:]))
	if err != nil || host == "" || port <= 0 || port > 65535 {
		return entry{}, false
	}

	// 成功日志里带着当时访问的目标，独立测速时优先复用它（RTSP/HTTP 都能对上）。
	target := ""
	if i := strings.Index(raw, "成功访问:"); i >= 0 {
		rest := strings.TrimSpace(raw[i+len("成功访问:"):])
		if fields := strings.Fields(rest); len(fields) > 0 {
			target = fields[0]
		}
	}

	addr := host + ":" + strconv.Itoa(port)
	return entry{proxyType: proxyType, ip: host, port: port, addr: addr, url: target}, true
}
