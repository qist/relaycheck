package parser
import (
	"bufio"
	"net"
	"os"
	"strconv"
	"strings"
	"log"
	"errors"
	"github.com/qist/relaycheck/config"
	"github.com/qist/relaycheck/worker"
	"github.com/qist/relaycheck/proxyscanner"
	
)


// 修改parseCIDRFile函数，使用新的代理检测逻辑
func ParseCIDRFile(wp *worker.WorkerPool, successfulIPsCh chan<- string) error {
	file, err := os.Open(config.Cfg.CIDRFile)
	if err != nil {
		return err
	}
	defer file.Close()

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
				for _, portStr := range config.Cfg.Ports{
					port, err := strconv.Atoi(portStr)
					if err != nil {
						log.Printf("端口转换失败 %s: %v\n", portStr, err)
						continue
					}
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
			for _, portStr := range config.Cfg.Ports {
				port, err := strconv.Atoi(portStr)
				if err != nil {
					log.Printf("端口转换失败 %s: %v\n", portStr, err)
					continue
				}
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
