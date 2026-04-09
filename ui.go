package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qist/relaycheck/config"
)

type uiJob struct {
	ID      string
	Created time.Time

	mu           sync.Mutex
	Output       strings.Builder
	SuccessCount int
	Successes    []string
	SuccessItems []uiSuccess
	Done         bool
	OK           bool
	Error        string
}

type uiServer struct {
	listenAddr string
	runSem     chan struct{}

	mu   sync.Mutex
	jobs map[string]*uiJob
}

func StartUI(listenAddr string) error {
	s := &uiServer{
		listenAddr: listenAddr,
		runSem:     make(chan struct{}, 1),
		jobs:       map[string]*uiJob{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/run", s.handleRun)
	mux.HandleFunc("/job", s.handleJob)
	mux.HandleFunc("/job/status", s.handleJobStatus)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	fmt.Printf("Web 界面已启动: http://%s/\n", listenAddr)
	return server.ListenAndServe()
}

func (s *uiServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type pageData struct {
		Version     string
		GoVersion   string
		RuntimeOS   string
		RuntimeArch string
	}

	tpl := template.Must(template.New("index").Parse(uiIndexHTML))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tpl.Execute(w, pageData{
		Version:     config.Version,
		GoVersion:   runtime.Version(),
		RuntimeOS:   runtime.GOOS,
		RuntimeArch: runtime.GOARCH,
	})
}

func (s *uiServer) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	select {
	case s.runSem <- struct{}{}:
	default:
		http.Error(w, "已有任务在运行，请等待完成后再提交", http.StatusTooManyRequests)
		return
	}

	job := &uiJob{
		ID:      randID(8),
		Created: time.Now(),
	}
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()

	form := uiScanForm{
		CIDRText:              r.FormValue("cidr"),
		PortsText:             r.FormValue("ports"),
		URLPathsText:          r.FormValue("urlpaths"),
		ProxyTypesText:        r.FormValue("proxytypes"),
		MaxConcurrentRequests: r.FormValue("concurrency"),
		ProxyTimeoutSeconds:   r.FormValue("timeout"),
		RetryTimes:            r.FormValue("retryTimes"),
		RetryIntervalSeconds:  r.FormValue("retryIntervalSeconds"),
		ProxyAuthEnabled:      r.FormValue("authEnabled"),
		ProxyUsername:         r.FormValue("username"),
		ProxyPassword:         r.FormValue("password"),
		ValidateContent:       r.FormValue("validateContent"),
		LogEnabled:            r.FormValue("logEnabled"),
		UserAgent:             r.FormValue("ua"),
		RealIPAPIsText:        r.FormValue("realipApis"),
		IPInfoAPIsJSON:        r.FormValue("ipInfoApis"),
	}

	go func() {
		defer func() { <-s.runSem }()

		err := s.runScanJob(job, form)
		job.mu.Lock()
		job.Done = true
		if err != nil {
			job.OK = false
			job.Error = err.Error()
			job.Output.WriteString("\n任务失败: " + err.Error() + "\n")
		} else {
			job.OK = true
			job.Output.WriteString("\n任务完成\n")
		}
		job.mu.Unlock()
	}()

	http.Redirect(w, r, "/job?id="+job.ID, http.StatusSeeOther)
}

func (s *uiServer) runScanJob(job *uiJob, form uiScanForm) error {
	cfg, cidrTempPath, err := form.toConfig()
	if err != nil {
		return err
	}
	defer func() {
		if cidrTempPath != "" {
			_ = os.Remove(cidrTempPath)
		}
	}()

	jobAppend := func(p []byte) {
		job.mu.Lock()
		_, _ = job.Output.Write(p)
		job.mu.Unlock()
	}

	onSuccess := func(msg string) {
		msg = strings.TrimSpace(msg)
		if msg == "" {
			return
		}
		job.mu.Lock()
		job.SuccessCount++
		if len(job.Successes) < 2000 {
			job.Successes = append(job.Successes, msg)
			if item, ok := parseSuccessMsg(msg); ok {
				job.SuccessItems = append(job.SuccessItems, item)
			}
		}
		job.mu.Unlock()
	}

	return captureProcessOutput(func() error {
		config.Cfg = *cfg
		if !cfg.LogEnabled {
			log.SetOutput(io.Discard)
		}
		return runScan(cfg, onSuccess)
	}, jobAppend)
}

func (s *uiServer) handleJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	job := s.getJob(id)
	if job == nil {
		http.NotFound(w, r)
		return
	}

	type pageData struct {
		ID string
	}
	tpl := template.Must(template.New("job").Parse(uiJobHTML))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tpl.Execute(w, pageData{ID: job.ID})
}

func (s *uiServer) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	job := s.getJob(id)
	if job == nil {
		http.NotFound(w, r)
		return
	}

	job.mu.Lock()
	resp := map[string]any{
		"id":            job.ID,
		"created":       job.Created.Format(time.RFC3339),
		"done":          job.Done,
		"ok":            job.OK,
		"error":         job.Error,
		"output":        job.Output.String(),
		"success_count": job.SuccessCount,
		"successes":     job.Successes,
		"success_items": job.SuccessItems,
	}
	job.mu.Unlock()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *uiServer) getJob(id string) *uiJob {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

type uiScanForm struct {
	CIDRText              string
	PortsText             string
	URLPathsText          string
	ProxyTypesText        string
	MaxConcurrentRequests string
	ProxyTimeoutSeconds   string
	RetryTimes            string
	RetryIntervalSeconds  string
	ProxyAuthEnabled      string
	ProxyUsername         string
	ProxyPassword         string
	ValidateContent       string
	LogEnabled            string
	UserAgent             string
	RealIPAPIsText        string
	IPInfoAPIsJSON        string
}

func (f uiScanForm) toConfig() (*config.Config, string, error) {
	cidrText := strings.TrimSpace(f.CIDRText)
	if cidrText == "" {
		return nil, "", fmt.Errorf("CIDR/IP 输入不能为空")
	}
	tmp, err := os.CreateTemp("", "relaycheck-cidr-*.txt")
	if err != nil {
		return nil, "", err
	}
	if _, err := tmp.WriteString(cidrText + "\n"); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, "", err
	}
	_ = tmp.Close()

	ports := splitLinesOrComma(f.PortsText)
	if len(ports) == 0 {
		ports = []string{"1080", "7890", "443"}
	}

	urlPaths := splitLinesOrComma(f.URLPathsText)
	if len(urlPaths) == 0 {
		urlPaths = []string{"https://www.baidu.com"}
	}

	proxyTypes := splitLinesOrComma(f.ProxyTypesText)
	if len(proxyTypes) == 0 {
		proxyTypes = []string{"socks5", "http"}
	}

	concurrency := parseIntDefault(f.MaxConcurrentRequests, 100)
	timeout := parseIntDefault(f.ProxyTimeoutSeconds, 5)
	retryTimes := parseIntDefault(f.RetryTimes, 1)
	retryInterval := parseIntDefault(f.RetryIntervalSeconds, 1)

	cfg := &config.Config{
		Ports:                ports,
		URLPaths:             urlPaths,
		MaxConcurrentRequest: concurrency,
		SuccessfulIPsFile:    "",
		CIDRFile:             tmp.Name(),
		FileBufferSize:       concurrency * 4,
		LogEnabled:           parseBoolDefault(f.LogEnabled, true),
		ProxyTypes:           proxyTypes,
		ProxyAuthEnabled:     parseBoolDefault(f.ProxyAuthEnabled, false),
		ProxyUsername:        strings.TrimSpace(f.ProxyUsername),
		ProxyPassword:        strings.TrimSpace(f.ProxyPassword),
		ProxyTimeout:         timeout,
		ValidateContent:      parseBoolDefault(f.ValidateContent, false),
		RetryTimes:           retryTimes,
		RetryIntervalSeconds: retryInterval,
	}

	ua := strings.TrimSpace(f.UserAgent)
	if ua != "" {
		cfg.UAHeaders = map[string][]string{
			"User-Agent": {ua},
		}
	} else {
		cfg.UAHeaders = map[string][]string{
			"User-Agent": {"okhttp/3.12.0"},
		}
	}

	realAPIs := splitLinesOrComma(f.RealIPAPIsText)
	if len(realAPIs) > 0 {
		cfg.RealIPApiURLs = realAPIs
	} else {
		cfg.RealIPApiURLs = []string{
			"https://ip.zxinc.org/api.php",
			"https://myip.ipip.net",
			"https://ifconfig.me/ip",
		}
	}
	if strings.TrimSpace(f.IPInfoAPIsJSON) != "" {
		var apis []config.IPInfoAPIConfig
		if err := json.Unmarshal([]byte(f.IPInfoAPIsJSON), &apis); err == nil && len(apis) > 0 {
			cfg.IPInfoAPIs = apis
		} else {
			cfg.IPInfoAPIs = []config.IPInfoAPIConfig{
				{
					URL:          "https://opendata.baidu.com/api.php?co=&resource_id=6006&oe=utf8&query={ip}",
					CodeKey:      "status",
					ExpectedCode: "0",
					ProvinceKey:  "data.0.location",
					ISPKey:       "data.0.location",
				},
				{
					URL:          "https://mesh.if.iqiyi.com/aid/ip/info?version=1.1.1&ip={ip}",
					CodeKey:      "code",
					ExpectedCode: "0",
					ProvinceKey:  "data.provinceCN",
					ISPKey:       "data.ispCN",
				},
				{
					URL:          "https://ipinfo.io/{ip}",
					CodeKey:      "readme",
					ExpectedCode: "https://ipinfo.io/missingauth",
					ProvinceKey:  "city",
					ISPKey:       "org",
				},
			}
		}
	} else {
		cfg.IPInfoAPIs = []config.IPInfoAPIConfig{
			{
				URL:          "https://opendata.baidu.com/api.php?co=&resource_id=6006&oe=utf8&query={ip}",
				CodeKey:      "status",
				ExpectedCode: "0",
				ProvinceKey:  "data.0.location",
				ISPKey:       "data.0.location",
			},
			{
				URL:          "https://mesh.if.iqiyi.com/aid/ip/info?version=1.1.1&ip={ip}",
				CodeKey:      "code",
				ExpectedCode: "0",
				ProvinceKey:  "data.provinceCN",
				ISPKey:       "data.ispCN",
			},
			{
				URL:          "https://ipinfo.io/{ip}",
				CodeKey:      "readme",
				ExpectedCode: "https://ipinfo.io/missingauth",
				ProvinceKey:  "city",
				ISPKey:       "org",
			},
		}
	}

	return cfg, tmp.Name(), nil
}

func splitLinesOrComma(s string) []string {
	raw := strings.ReplaceAll(s, ",", "\n")
	parts := strings.Split(raw, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func parseIntDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func parseBoolDefault(s string, def bool) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return def
	}
	switch s {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func captureProcessOutput(fn func() error, onData func([]byte)) error {
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	oldLogWriter := log.Writer()
	oldLogFlags := log.Flags()
	oldLogPrefix := log.Prefix()

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return err
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW
	log.SetFlags(oldLogFlags)
	log.SetPrefix(oldLogPrefix)
	log.SetOutput(stderrW)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(writerFunc(onData), stdoutR)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(writerFunc(onData), stderrR)
	}()

	runErr := fn()

	_ = stdoutW.Close()
	_ = stderrW.Close()
	wg.Wait()
	_ = stdoutR.Close()
	_ = stderrR.Close()

	os.Stdout = oldStdout
	os.Stderr = oldStderr
	log.SetOutput(oldLogWriter)
	log.SetFlags(oldLogFlags)
	log.SetPrefix(oldLogPrefix)

	return runErr
}

type writerFunc func([]byte)

func (f writerFunc) Write(p []byte) (int, error) {
	f(p)
	return len(p), nil
}

func randID(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type uiSuccess struct {
	ProxyType   string `json:"proxyType"`
	ProxyAddr   string `json:"proxyAddr"`
	ProxyGeo    string `json:"proxyGeo"`
	ExitIP      string `json:"exitIp"`
	ExitGeo     string `json:"exitGeo"`
	URL         string `json:"url"`
	ElapsedText string `json:"elapsed"`
}

func parseSuccessMsg(s string) (uiSuccess, bool) {
	const (
		tagProxy   = "代理:"
		tagExit    = "出口IP:"
		tagAccess  = "成功访问:"
		tagElapsed = "耗时:"
	)

	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "可用") {
		return uiSuccess{}, false
	}
	iProxy := strings.Index(s, tagProxy)
	iExit := strings.Index(s, tagExit)
	iAccess := strings.Index(s, tagAccess)
	iElapsed := strings.Index(s, tagElapsed)
	if iProxy < 0 || iExit < 0 || iAccess < 0 || iElapsed < 0 {
		return uiSuccess{}, false
	}
	if !(iProxy < iExit && iExit < iAccess && iAccess < iElapsed) {
		return uiSuccess{}, false
	}

	proxyType := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s[:iProxy], "可用"), "代理"))

	left := strings.TrimSpace(s[iProxy+len(tagProxy) : iExit])
	right := strings.TrimSpace(s[iExit+len(tagExit) : iAccess])
	url := strings.TrimSpace(s[iAccess+len(tagAccess) : iElapsed])
	elapsed := strings.TrimSpace(s[iElapsed+len(tagElapsed):])

	leftParts := strings.Fields(left)
	rightParts := strings.Fields(right)
	if len(leftParts) < 1 || len(rightParts) < 1 {
		return uiSuccess{}, false
	}

	proxyAddr := leftParts[0]
	proxyGeo := strings.TrimSpace(strings.Join(leftParts[1:], " "))
	exitIP := rightParts[0]
	exitGeo := strings.TrimSpace(strings.Join(rightParts[1:], " "))

	return uiSuccess{
		ProxyType:   proxyType,
		ProxyAddr:   proxyAddr,
		ProxyGeo:    proxyGeo,
		ExitIP:      exitIP,
		ExitGeo:     exitGeo,
		URL:         url,
		ElapsedText: elapsed,
	}, true
}

const uiIndexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Relaycheck</title>
  <style>
    body { font-family: system-ui, -apple-system, Segoe UI, Roboto, sans-serif; margin: 24px; }
    h1 { margin: 0 0 8px; }
    .meta { color: #555; margin-bottom: 18px; }
    .card { border: 1px solid #ddd; border-radius: 10px; padding: 16px; margin: 14px 0; }
    label { display:block; margin: 10px 0 4px; }
    input, textarea { width: 100%; padding: 8px; box-sizing: border-box; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace; }
    textarea { min-height: 160px; }
    button { padding: 10px 14px; cursor: pointer; }
    .row { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
    @media (max-width: 900px) { .row { grid-template-columns: 1fr; } }
    .hint { color: #777; font-size: 12px; }
  </style>
</head>
<body>
  <h1>Relaycheck Web</h1>
  <div class="meta">
    版本: {{.Version}} · Go: {{.GoVersion}} · 当前运行: {{.RuntimeOS}}/{{.RuntimeArch}}
  </div>

  <div class="card">
    <h2>扫描参数</h2>
    <form method="post" action="/run">
      <label>CIDR / IP / IP:PORT（每行一条）</label>
      <textarea name="cidr" placeholder="例如:
1.2.3.4
1.2.3.0/24
1.2.3.4:7890
[2001:db8::1]:7890"></textarea>

      <div class="row">
        <div>
          <label>端口（支持范围 7890-7900，逗号/换行分隔）</label>
          <input name="ports" value="1080,7890,443" />
        </div>
        <div>
          <label>代理类型（逗号/换行分隔）</label>
          <input name="proxytypes" value="socks5,http" />
        </div>
      </div>

      <label>URLPaths（逗号/换行分隔）</label>
      <input name="urlpaths" value="https://www.baidu.com" />

      <div class="row">
        <div>
          <label>并发（maxConcurrentRequests）</label>
          <input name="concurrency" value="100" />
        </div>
        <div>
          <label>超时秒（proxyTimeout）</label>
          <input name="timeout" value="5" />
        </div>
      </div>

      <div class="row">
        <div>
          <label>重试次数（retryTimes）</label>
          <input name="retryTimes" value="1" />
        </div>
        <div>
          <label>重试间隔秒（retryIntervalSeconds）</label>
          <input name="retryIntervalSeconds" value="1" />
        </div>
      </div>

      <div class="row">
        <div>
          <label>代理认证（authEnabled：true/false）</label>
          <input name="authEnabled" value="false" />
        </div>
        <div>
          <label>内容校验（validateContent：true/false）</label>
          <input name="validateContent" value="false" />
        </div>
      </div>

      <div class="row">
        <div>
          <label>用户名（username）</label>
          <input name="username" value="" />
        </div>
        <div>
          <label>密码（password）</label>
          <input name="password" value="" />
        </div>
      </div>

      <label>显示日志（logEnabled：true/false）</label>
      <input name="logEnabled" value="true" />
      <div class="hint">同一时间只允许跑一个任务（因为扫描参数是全局的）。</div>

      <div style="margin-top: 14px"></div>
      <h3>高级（UA/出口IP/归属地）</h3>
      <div class="hint">如果不填写 UA / 出口IP接口 / 归属地接口，将显示“未知”。</div>
      <label>User-Agent（ua，可选）</label>
      <input name="ua" value="okhttp/3.12.0" />
      <div class="row">
        <div>
          <label>出口IP接口 RealIPApiURLs（逗号/换行分隔）</label>
          <textarea name="realipApis">https://ip.zxinc.org/api.php
https://myip.ipip.net
https://ifconfig.me/ip</textarea>
        </div>
        <div>
          <label>归属地接口 ip_info_apis（JSON 数组）</label>
          <textarea name="ipInfoApis">[
  {"url":"https://opendata.baidu.com/api.php?co=&resource_id=6006&oe=utf8&query={ip}","code_key":"status","expected_code":"0","province_key":"data.0.location","isp_key":"data.0.location"},
  {"url":"https://mesh.if.iqiyi.com/aid/ip/info?version=1.1.1&ip={ip}","code_key":"code","expected_code":"0","province_key":"data.provinceCN","isp_key":"data.ispCN"},
  {"url":"https://ipinfo.io/{ip}","code_key":"readme","expected_code":"https://ipinfo.io/missingauth","province_key":"city","isp_key":"org"}
]</textarea>
        </div>
      </div>

      <div style="margin-top: 12px">
        <button type="submit">开始扫描</button>
      </div>
    </form>
  </div>
</body>
</html>`

const uiJobHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>任务 {{.ID}}</title>
  <style>
    body { font-family: system-ui, -apple-system, Segoe UI, Roboto, sans-serif; margin: 24px; }
    h2 { margin: 18px 0 10px; }
    pre { background: #0b1020; color: #e6e6e6; padding: 14px; border-radius: 10px; overflow:auto; max-height: 70vh; }
    .muted { color:#666; }
    .okbox { background: #071a0f; color: #d2f7df; padding: 14px; border-radius: 10px; overflow:auto; max-height: 45vh; white-space: pre-wrap; }
    table { width: 100%; border-collapse: collapse; }
    th, td { border-bottom: 1px solid #eee; padding: 8px; text-align: left; vertical-align: top; }
  </style>
</head>
<body>
  <div><a href="/">返回</a></div>
  <h1>任务 {{.ID}}</h1>
  <div class="muted" id="status"></div>
  <h2 id="succTitle">成功</h2>
  <div class="muted" id="succSummary"></div>
  <table id="succTable">
    <thead>
      <tr>
        <th>类型</th>
        <th>代理IP</th>
        <th>代理归属地</th>
        <th>出口IP</th>
        <th>出口归属地</th>
        <th>URL</th>
        <th>耗时</th>
      </tr>
    </thead>
    <tbody id="succTbody"></tbody>
  </table>
  <h2>日志</h2>
  <pre id="out"></pre>
  <script>
    let lastSuccRows = -1;
    async function tick() {
      const resp = await fetch('/job/status?id={{.ID}}');
      if (!resp.ok) return;
      const data = await resp.json();
      document.getElementById('out').textContent = data.output || '';
      let status = data.done ? (data.ok ? '完成' : '失败') : '运行中';
      if (data.error) status += ' · ' + data.error;
      document.getElementById('status').textContent = status;
      const succCount = data.success_count || 0;
      document.getElementById('succSummary').textContent = '成功数量: ' + succCount + (succCount > 2000 ? '（仅展示前 2000 条）' : '');
      const items = Array.isArray(data.success_items) ? data.success_items : [];
      if (items.length !== lastSuccRows) {
        lastSuccRows = items.length;
        const tbody = document.getElementById('succTbody');
        while (tbody.firstChild) tbody.removeChild(tbody.firstChild);
        for (const it of items) {
          const tr = document.createElement('tr');
          const cols = [
            it.proxyType || '',
            it.proxyAddr || '',
            it.proxyGeo || '',
            it.exitIp || '',
            it.exitGeo || '',
            it.url || '',
            it.elapsed || '',
          ];
          for (const c of cols) {
            const td = document.createElement('td');
            td.textContent = c;
            tr.appendChild(td);
          }
          tbody.appendChild(tr);
        }
      }
      if (!data.done) setTimeout(tick, 800);
    }
    tick();
  </script>
</body>
</html>`
