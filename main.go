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

func main() {
	// 使用flag包解析命令行参数
	configFile := flag.String("config", "config.yaml", "配置文件的路径")
	Clash := flag.Bool("clash", false, "是否使用 Clash")
	Tvgate := flag.Bool("tvgate", false, "是否使用 TVGate")
	inputFile := flag.String("input", "", "输入日志文件路径，默认 successfulIPsFile 配置中的路径")
	outputFile := flag.String("output", "", "输出 YAML 文件路径，默认 filtered_proxies.yaml")
	namePrefix := flag.String("name", "广东电信", "Clash TVGate YAML name 前缀")
	maxSec := flag.Float64("maxsec", 0, "最大耗时秒数，0 表示不过滤")
	VersionFlag := flag.Bool("version", false, "显示程序版本")
	uiFlag := flag.Bool("ui", false, "启动 Web 界面")
	listenAddr := flag.String("listen", "127.0.0.1:8080", "Web 界面监听地址（仅在 -ui 时生效）")
	flag.Parse()
	if *VersionFlag {
		fmt.Println("程序版本:", config.Version)
		return
	}

	if *uiFlag {
		if err := StartUI(*listenAddr); err != nil {
			log.Fatalf("启动 Web 界面失败: %v", err)
		}
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
		if !cfg.LogEnabled {
			log.SetOutput(io.Discard)
		}
		err = runScan(cfg, nil)
		if err != nil {
			log.Printf("扫描失败: %v\n", err)
			return
		}
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

func runScan(cfg *config.Config, onSuccess func(msg string)) error {
	if cfg.ProxyTimeout <= 0 {
		cfg.ProxyTimeout = 5
	}

	if cfg.MaxConcurrentRequest <= 0 {
		cfg.MaxConcurrentRequest = 100
	}
	if cfg.FileBufferSize <= 0 {
		cfg.FileBufferSize = 1024
	}

	if cfg.SuccessfulIPsFile != "" {
		if err := utils.ClearFileContent(cfg.SuccessfulIPsFile); err != nil {
			return err
		}
	}

	successfulIPsCh := make(chan string, cfg.FileBufferSize)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for successfulIP := range successfulIPsCh {
			if onSuccess != nil {
				onSuccess(successfulIP)
			}
			if cfg.SuccessfulIPsFile != "" {
				if err := utils.AppendToFile(cfg.SuccessfulIPsFile, successfulIP); err != nil {
					log.Printf("写入成功的IP到文件失败: %v\n", err)
				}
			}
		}
	}()

	bufferSize := cfg.MaxConcurrentRequest * 1024
	workerPool := worker.NewWorkerPool(cfg.MaxConcurrentRequest, bufferSize)
	workerPool.Start()

	if err := parser.ParseCIDRFile(workerPool, successfulIPsCh); err != nil {
		return err
	}

	workerPool.Close()
	workerPool.Wait()

	close(successfulIPsCh)
	wg.Wait()
	return nil
}
