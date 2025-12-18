## 代理扫描

### 配置文件说明（config.yaml）

本项目所有参数均可通过 `config.yaml` 配置，主要字段如下：

- **ports**  
	需要扫描的端口列表，支持多个端口。例如：`[1080, 443, 7890]`

- **urlPaths**  
	需要测试的目标URL列表，支持多种协议（http/https/rtsp等）。

- **proxyTypes**  
	代理类型，支持 `socks5`、`socks4a`、`http`、`https`。

- **proxyAuthEnabled**  
	是否启用代理认证（布尔值）。

- **proxyUsername / proxyPassword**  
	代理认证用户名和密码。

- **proxyTimeout**  
	单次代理请求超时时间（秒）。

- **validateContent**  
	是否校验返回内容。`false` 表示只要HTTP状态码为200即视为成功。

- **retryTimes / retryIntervalSeconds**  
	失败重试次数与重试间隔（秒）。

- **RealIPApiURLs**  
	用于获取出口IP的API地址列表。

- **ip_info_apis**  
	IP归属地查询API配置，支持自定义字段映射。

- **uaHeaders**  
	自定义请求头（User-Agent等），支持多值。

- **HttpProxy**  
	HTTP代理配置，`Enabled` 控制是否启用，`headers` 可自定义代理请求头。

- **maxConcurrentRequests**  
	最大并发请求数。

- **successfulIPsFile**  
	扫描成功IP的输出文件名。

- **cidrFile**  
	扫描的IP地址/网段/端口文件路径，支持CIDR、单IP、IP:端口格式。

- **FileBufferSize**  
	文件缓冲区大小。

- **logEnabled**  
	是否开启日志显示。

如需详细参数示例，请参考 `config.yaml` 文件中的注释和样例内容。

---

### main.go 命令行参数说明

程序支持以下命令行参数：

- `-config`  指定配置文件路径，默认 `config.yaml`
- `-clash`   是否使用 Clash 逻辑，布尔值，默认 false
- `-tvgate`  是否使用 TVGate 逻辑，布尔值，默认 false
- `-input`   输入日志文件路径，默认读取配置文件 `successfulIPsFile` 字段
- `-output`  输出 YAML 文件路径，默认 `filtered_proxies.yaml`
- `-name`    Clash YAML name 前缀，默认 `广东电信`
- `-maxsec`  最大耗时秒数，0 表示不过滤

示例：

```bash
# 验证可用代理
## 当前目录执行
./relaycheck-linux-amd64 
## 指定配置文件执行
./relaycheck-linux-amd64 -config=config.yaml
## 生成Clash默认
./relaycheck-linux-amd64 -clash
## 生成Clash，过滤耗时超过2秒的代理
./relaycheck-linux-amd64 -config=config.yaml -clash -input=successful_zubo.txt -output=clash.yaml -name="广东电信" -maxsec=2
## 生成TVgate默认
./relaycheck-linux-amd64 -tvgate
## 生成TVgate，过滤耗时超过2秒的代理
./relaycheck-linux-amd64 -config=config.yaml -tvgate -input=successful_zubo.txt -output=tvgate.yaml -name="广东电信" -maxsec=2
```