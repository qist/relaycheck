package config

import (
	_ "embed"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// --- 代理访问统一模块 ---
type HttpProxyConfig struct {
	Enabled bool              `yaml:"Enabled"`
	Headers map[string]string `yaml:"headers"`
}

type Config struct {
	Ports                []string            `yaml:"ports"`
	URLPaths             []string            `yaml:"urlPaths"`
	MaxConcurrentRequest int                 `yaml:"maxConcurrentRequests"`
	SuccessfulIPsFile    string              `yaml:"successfulIPsFile"`
	CIDRFile             string              `yaml:"cidrFile"`
	FileBufferSize       int                 `yaml:"filebufferSize"`
	LogEnabled           bool                `yaml:"logEnabled"`
	ProxyTypes           []string            `yaml:"proxyTypes"`
	ProxyAuthEnabled     bool                `yaml:"proxyAuthEnabled"`
	ProxyUsername        string              `yaml:"proxyUsername"`
	ProxyPassword        string              `yaml:"proxyPassword"`
	ProxyTimeout         int                 `yaml:"proxyTimeout"`
	HttpProxy            *HttpProxyConfig    `yaml:"HttpProxy"`
	UAHeaders            map[string][]string `yaml:"uaHeaders"`
	ValidateContent      bool                `yaml:"validateContent"`
	RealIPApiURLs        []string            `yaml:"RealIPApiURLs"`
	IPInfoAPIs           []IPInfoAPIConfig   `yaml:"ip_info_apis"`
	RetryTimes           int                 `yaml:"retryTimes"`
	RetryIntervalSeconds int                 `yaml:"retryIntervalSeconds"`
	SpeedTest            *SpeedTestConfig    `yaml:"speedTest"`
}

// SpeedTestConfig 控制扫描命中后的吞吐测速。
// 未配置时默认开启：测速算法与 tvgate 一致，读取完整分片后只统计
// 最后 3 秒的速率，避免前段突发把限速/停住的出口测成高带宽。
type SpeedTestConfig struct {
	Enabled      bool `yaml:"enabled"`
	Timeout      int  `yaml:"timeout"`      // 单次测速超时（秒）
	SampleSizeMB int  `yaml:"sampleSizeMB"` // 正文采样上限（MB），需覆盖整个分片
	Concurrency  int  `yaml:"concurrency"`  // -speed 模式的并发数，默认 8
}

type IPInfoAPIConfig struct {
	URL          string `yaml:"url" json:"url"`
	Type         string `yaml:"type" json:"type"`                   // "json" (默认) 或 "html"
	Regexp       string `yaml:"regexp" json:"regexp"`               // HTML 模式下的正则，需含 (?P<province>...) 和 (?P<isp>...) 命名捕获组
	CodeKey      string `yaml:"code_key" json:"code_key"`
	ExpectedCode string `yaml:"expected_code" json:"expected_code"`
	ProvinceKey  string `yaml:"province_key" json:"province_key"`
	ISPKey       string `yaml:"isp_key" json:"isp_key"`
}

var Cfg Config

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(data, &Cfg)
	if err != nil {
		return nil, err
	}
	return &Cfg, nil
}

//go:embed version
var versionFile string

// Version 是程序版本号，默认从 version 文件读取
// 可以在编译时通过 -ldflags 覆盖，例如:
// go build -ldflags "-X 'github.com/qist/relaycheck/config.Version=v1.0.0'" .
var Version = ""

func init() {
	// 如果 Version 没被 ldflags 覆盖，则用 embed 文件内容
	if Version == "" {
		Version = strings.TrimSpace(versionFile)
	}
}
