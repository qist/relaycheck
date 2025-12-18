package config

import (
	"gopkg.in/yaml.v3"
	"os"
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
}

type IPInfoAPIConfig struct {
	URL          string `yaml:"url"`
	CodeKey      string `yaml:"code_key"`
	ExpectedCode string `yaml:"expected_code"`
	ProvinceKey  string `yaml:"province_key"`
	ISPKey       string `yaml:"isp_key"`
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
