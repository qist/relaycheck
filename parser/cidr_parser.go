package parser

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/qist/relaycheck/config"
	"github.com/qist/relaycheck/proxyscanner"
	"github.com/qist/relaycheck/worker"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
)

// 修改parseCIDRFile函数，使用新的代理检测逻辑
func ParseCIDRFile(wp *worker.WorkerPool, successfulIPsCh chan<- string) error {
	file, err := os.Open(config.Cfg.CIDRFile)
	if err != nil {
		return err
	}
	defer file.Close()
	ports, err := expandPorts(config.Cfg.Ports)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 优先判断是否为带端口的 IP（IPv4:port 或 [IPv6]:port）
		host, portStr, err := net.SplitHostPort(line)
		if err == nil {
			// 标准格式成功解析
			port, err := strconv.Atoi(portStr)
			if err == nil {
				wp.AddTask(worker.Task{
					IP:       formatIPForHostPort(host),
					Port:     port,
					Executor: func(ip string, port int) { proxyscanner.TestProxy(ip, port, successfulIPsCh) },
				})
				continue
			}
		}

		// 非标准 IPv6:port 解析支持
		if ip, port, err := tryParseIPv6WithPort(line); err == nil {
			wp.AddTask(worker.Task{
				IP:       formatIPForHostPort(ip),
				Port:     port,
				Executor: func(ip string, port int) { proxyscanner.TestProxy(ip, port, successfulIPsCh) },
			})
			continue
		}

		// 判断是否为 CIDR（支持 IPv6 CIDR）
		if _, ipnet, err := net.ParseCIDR(line); err == nil {
			ips := getAllIPsInRange(ipnet)
			for _, ip := range ips {
				formattedIP := formatIPForHostPort(ip)
				for _, port := range ports {
					wp.AddTask(worker.Task{
						IP:       formattedIP,
						Port:     port,
						Executor: func(ip string, port int) { proxyscanner.TestProxy(ip, port, successfulIPsCh) },
					})
				}
			}
			continue
		}

		// 判断是否为普通 IP（IPv4 或 IPv6）
		if net.ParseIP(line) != nil {
			formattedIP := formatIPForHostPort(line)
			for _, port := range ports {
				wp.AddTask(worker.Task{
					IP:       formattedIP,
					Port:     port,
					Executor: func(ip string, port int) { proxyscanner.TestProxy(ip, port, successfulIPsCh) },
				})
			}
			continue
		}

		log.Printf("无法识别的行: %s\n", line)
	}

	return scanner.Err()
}

// 尝试将 IPv6:port 非标准形式，如 2001:db8::1:7890 拆成 IP 和端口
func tryParseIPv6WithPort(s string) (string, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return "", 0, errors.New("不是合法 IPv6:port")
	}

	// 从末尾尝试将最后一段解析为端口
	portPart := parts[len(parts)-1]
	ipPart := strings.Join(parts[:len(parts)-1], ":")

	port, err := strconv.Atoi(portPart)
	if err != nil {
		return "", 0, err
	}

	// 验证 ipPart 是合法 IPv6
	ip := net.ParseIP(ipPart)
	if ip == nil || ip.To4() != nil {
		return "", 0, errors.New("不是合法 IPv6")
	}

	return ipPart, port, nil
}

// 如果是 IPv6（含冒号）且未加 [] 包裹，则包裹起来
func formatIPForHostPort(ip string) string {
	if strings.Contains(ip, ":") && !strings.HasPrefix(ip, "[") {
		return "[" + ip + "]"
	}
	return ip
}

// getAllIPsInRange 返回CIDR范围内的所有IP地址
func getAllIPsInRange(ipnet *net.IPNet) []string {
	var ips []string

	// 拷贝起始 IP，避免修改原始 ipnet.IP
	startIP := make(net.IP, len(ipnet.IP))
	copy(startIP, ipnet.IP)

	for ip := startIP; ipnet.Contains(ip); incIP(ip) {
		ips = append(ips, ip.String())
	}

	return ips
}

// 自增 IP 地址（支持 IPv4 和 IPv6）
func incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}

// expandPorts 解析端口配置，支持:
func expandPorts(portRanges []string) ([]int, error) {
	var ports []int

	for _, raw := range portRanges {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		// 端口范围
		if strings.Contains(raw, "-") {
			parts := strings.SplitN(raw, "-", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("无效的端口范围: %s", raw)
			}

			start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))

			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("无效的端口范围: %s", raw)
			}

			if start <= 0 || end > 65535 || start > end {
				return nil, fmt.Errorf("端口范围越界: %s", raw)
			}

			for p := start; p <= end; p++ {
				ports = append(ports, p)
			}
			continue
		}

		// 单端口
		port, err := strconv.Atoi(raw)
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("无效的端口: %s", raw)
		}

		ports = append(ports, port)
	}

	return ports, nil
}
