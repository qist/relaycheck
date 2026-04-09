## 编译

```bash
git clone https://github.com/qist/relaycheck.git
cd relaycheck
make 
清理 编译 
make clean
```

## 代理扫描&验证工具

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
- `-ui`      启动 Web 界面（表单填写扫描参数）
- `-listen`  Web 界面监听地址（仅在 `-ui` 时生效），默认 `127.0.0.1:8080`
- `-version` 显示程序版本

示例：

```bash
# 验证可用代理
## 当前目录执行
./Relaycheck-linux-amd64 
## 指定配置文件执行
./Relaycheck-linux-amd64 -config=config.yaml
## 生成Clash默认
./Relaycheck-linux-amd64 -clash
## 生成Clash，过滤耗时超过2秒的代理
./Relaycheck-linux-amd64 -config=config.yaml -clash -input=successful_zubo.txt -output=clash.yaml -name="广东电信" -maxsec=2
## 生成TVgate默认
./Relaycheck-linux-amd64 -tvgate
## 生成TVgate，过滤耗时超过2秒的代理
./Relaycheck-linux-amd64 -config=config.yaml -tvgate -input=successful_zubo.txt -output=tvgate.yaml -name="广东电信" -maxsec=2

## 启动 Web 界面（默认仅本机访问）
./Relaycheck-linux-amd64 -ui
## 指定监听地址
./Relaycheck-linux-amd64 -ui -listen=0.0.0.0:8080
```

---

### Web 界面说明

启动后访问 `http://127.0.0.1:8080/`（或你指定的 `-listen` 地址）。

Web 界面支持：

- 表单填写扫描参数（不读取 `config.yaml`）
	- CIDR / IP / IP:PORT（每行一条）
	- 端口（支持范围，例如 `7890-7900`；逗号/换行分隔）
	- 代理类型（例如 `socks5,http`）
	- URLPaths（例如 `https://www.baidu.com`）
	- 并发、超时、重试次数与间隔
	- 可选代理认证
- 高级参数（可编辑，默认已填好）
	- User-Agent：默认 `okhttp/3.12.0`
	- RealIPApiURLs：出口 IP 获取接口列表（数组按顺序尝试，失败自动切换下一个）
	- ip_info_apis：归属地查询接口列表（数组按顺序尝试，失败自动切换下一个）
- 扫描过程中实时显示输出
- 成功结果独立显示（成功数量 + 成功表格：类型/代理IP/归属地/出口IP/出口归属地/URL/耗时）

注意：

- Web 界面不读取 `config.yaml`，所有参数以页面表单为准
- `ip_info_apis` 在 Web 中以 JSON 数组形式填写，字段名与 `config.yaml` 一致：
	- `url`、`code_key`、`expected_code`、`province_key`、`isp_key`
