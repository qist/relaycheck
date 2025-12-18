package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/qist/relaycheck/config"
	"github.com/qist/relaycheck/mihomo"
	"github.com/qist/relaycheck/parser"
	"github.com/qist/relaycheck/tvgate"
	"github.com/qist/relaycheck/utils"
	"github.com/qist/relaycheck/worker"
)

var successfulIPsCh chan string
var workerPool *worker.WorkerPool
var VersionFlag *bool

func main() {
	// 使用flag包解析命令行参数
	configFile := flag.String("config", "config.yaml", "配置文件的路径")
	Clash := flag.Bool("clash", false, "是否使用 Clash")
	Tvgate := flag.Bool("tvgate", false, "是否使用 TVGate")
	inputFile := flag.String("input", "", "输入日志文件路径，默认 successfulIPsFile 配置中的路径")
	outputFile := flag.String("output", "", "输出 YAML 文件路径，默认 filtered_proxies.yaml")
	namePrefix := flag.String("name", "广东电信", "Clash TVGate YAML name 前缀")
	maxSec := flag.Float64("maxsec", 0, "最大耗时秒数，0 表示不过滤")
	VersionFlag = flag.Bool("version", false, "显示程序版本")
	flag.Parse()
	if *VersionFlag {
		fmt.Println("程序版本:", config.Version)
		return
	}
	start := time.Now() // 记录开始时间
	fmt.Println("扫描开始: ", time.Now().Format("2006-01-02 15:04:05"))
	// 加载配置文件
	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		fmt.Println("加载配置文件失败:", err)
		return
	}
	if *Clash || *Tvgate {
		// -------------------------
		if *Clash {
			// 直接调用 GenerateClashYAML，函数内部已经处理默认值
			err = mihomo.GenerateClashYAML(*inputFile, *outputFile, *namePrefix, *maxSec, cfg)
			if err != nil {
				log.Fatalf("生成 Clash YAML 失败: %v", err)
			}
			fmt.Println("生成完成！")
		}
		if *Tvgate {
			// 直接调用 GenerateClashYAML，函数内部已经处理默认值
			err = tvgate.GenerateTVGateYAML(*inputFile, *outputFile, *namePrefix, *maxSec, cfg)
			if err != nil {
				log.Fatalf("生成 Clash YAML 失败: %v", err)
			}
			fmt.Println("生成完成！")
		}
		// -------------------------
	} else {
		// 设置默认超时（如未配置）
		if cfg.ProxyTimeout <= 0 {
			cfg.ProxyTimeout = 5 // 默认5秒
		}

		// 设置日志记录器
		if !cfg.LogEnabled {
			log.SetOutput(io.Discard)
		}
		// 清空文件内容
		err = utils.ClearFileContent(cfg.SuccessfulIPsFile)
		if err != nil {
			log.Printf("清空文件内容失败: %v\n", err)
			return
		}
		successfulIPsCh = make(chan string, cfg.FileBufferSize)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for successfulIP := range successfulIPsCh {
				err := utils.AppendToFile(cfg.SuccessfulIPsFile, successfulIP)
				if err != nil {
					log.Printf("写入成功的IP到文件失败: %v\n", err)
				}
			}
		}()

		// 设置线程池大小
		var BufferSize = cfg.MaxConcurrentRequest * 1024
		// 创建并启动 worker pool
		workerPool = worker.NewWorkerPool(cfg.MaxConcurrentRequest, BufferSize)
		workerPool.Start()
		// 解析 CIDR 文件并直接添加任务到 worker pool
		err = parser.ParseCIDRFile(workerPool, successfulIPsCh)
		if err != nil {
			log.Printf("解析CIDR文件失败: %v\n", err)
			return
		}

		// Task 向管道写入完关闭管道
		workerPool.Close()
		// // 等待所有任务完成
		workerPool.Wait()
		// 关闭成功 IP 通道
		close(successfulIPsCh)
		wg.Wait()
		// 删除所有以 "stream9527_" 开头的文件
		err = utils.DeleteStreamFiles()
		if err != nil {
			log.Fatalf("删除文件失败: %v", err)
		}

		elapsed := time.Since(start) // 计算并获取已用时间
		fmt.Println("总扫描时间: ", elapsed)
		fmt.Println("扫描结束: ", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Println("扫描完成请看文件:", cfg.SuccessfulIPsFile)
	}
}
