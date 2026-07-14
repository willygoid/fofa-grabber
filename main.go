package main

import (
	"bufio"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	Green  = "\033[92m"
	Red    = "\033[91m"
	Yellow = "\033[93m"
	Cyan   = "\033[36m"
	White  = "\033[97m"
	Reset  = "\033[0m"
	Bold   = "\033[1m"
)

type StickyBar struct {
	total    int
	current  int
	barWidth int
	size     int
	mu       sync.Mutex
}

func newStickyBar(total, size int) *StickyBar {
	return &StickyBar{total: total, barWidth: 50, size: size}
}

func (b *StickyBar) render() {
	pct := 0.0
	filled := 0
	if b.total > 0 {
		pct = float64(b.current) / float64(b.total) * 100
		filled = int(float64(b.barWidth) * float64(b.current) / float64(b.total))
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", b.barWidth-filled)
	fmt.Printf("\r\033[K%s FoFa API Grabber v1.3.1 by @willygoid | Total Size: %d %s\n",
		Cyan+Bold, b.size, Reset)
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

func PrintBanner() {
	banner := `
 _   _             _____           _     
| | | |           |_   _|         | |    
| |_| |_  ___ __    | | ___   ___ | |___ 
|  _  \ \/ / '__|   | |/ _ \ / _ \| / __|
| | | |>  <| |      | | (_) | (_) | \__ \
\_| |_/_/\_\_|      \_/\___/ \___/|_|___/
		FoFa API Grabber v1.3.1
		by @willygoid
`
	fmt.Println(banner)
}

// FofaResponse represents the API response structure
type FofaResponse struct {
	Error           bool       `json:"error"`
	ConsumedFpoint  int        `json:"consumed_fpoint"`
	RequiredFpoints int        `json:"required_fpoints"`
	Size            int        `json:"size"`
	Tip             string     `json:"tip"`
	Page            int        `json:"page"`
	Mode            string     `json:"mode"`
	Query           string     `json:"query"`
	Results         [][]string `json:"results"`
}

// CombinedResults untuk menggabungkan semua hasil
type CombinedResults struct {
	TotalSize    int        `json:"total_size"`
	TotalPages   int        `json:"total_pages"`
	Query        string     `json:"query"`
	FetchedAt    string     `json:"fetched_at"`
	AllResults   [][]string `json:"all_results"`
	ResultsCount int        `json:"results_count"`
}

// Config holds environment configuration
type Config struct {
	APIKey             string
	APIBase            string
	DelaySeconds       int
	ConcurrentRequests int
}

// sessionFile stores progress so an interrupted scan (crash/power loss) can be resumed
const sessionFile = ".fofa_session.json"

// SessionState persists the last query and the last completed page
type SessionState struct {
	Query       string `json:"query"`
	Method      string `json:"method"`
	FullSearch  bool   `json:"full_search"`
	LastPage    int    `json:"last_page"`
	TotalPages  int    `json:"total_pages"`
	TotalSize   int    `json:"total_size"`
	FailedPages []int  `json:"failed_pages"`
	DomainFile  string `json:"domain_file"`
	CSVFile     string `json:"csv_file"`
	Timestamp   string `json:"timestamp"`
	UpdatedAt   string `json:"updated_at"`
}

func main() {
	PrintBanner()
	// Load configuration from .env file
	config, err := loadConfig(".env")
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		fmt.Println("Creating default .env file...")
		createDefaultEnv()
		fmt.Println("Please edit .env file with your configuration and run again.")
		os.Exit(1)
	}

	// Load previous session state (for resume after crash/power loss)
	prevSession, _ := loadSessionState(sessionFile)
	if prevSession != nil {
		fmt.Printf("\n%s[i] Previous session found:%s\n", Cyan+Bold, Reset)
		fmt.Printf("    Query    : %s\n", prevSession.Query)
		fmt.Printf("    Method   : %s\n", prevSession.Method)
		fmt.Printf("    Progress : page %d / %d\n", prevSession.LastPage, prevSession.TotalPages)
		if len(prevSession.FailedPages) > 0 {
			fmt.Printf("%s    Failed   : %v%s\n", Yellow, prevSession.FailedPages, Reset)
		}
		fmt.Printf("    Updated  : %s\n", prevSession.UpdatedAt)
		if prevSession.LastPage < prevSession.TotalPages {
			fmt.Printf("%s    -> You can resume from page %d%s\n", Yellow, prevSession.LastPage+1, Reset)
		} else {
			fmt.Printf("%s    -> This session appears complete%s\n", Green, Reset)
		}
	}

	scanner := bufio.NewScanner(os.Stdin)

	// Offer a retry-only mode when the previous session left failed pages
	retryOnly := false
	if prevSession != nil && len(prevSession.FailedPages) > 0 {
		fmt.Printf("\n%s[?] Retry ONLY the %d failed page(s) %v from the previous session? [y/N]: %s",
			Cyan+Bold, len(prevSession.FailedPages), prevSession.FailedPages, Reset)
		scanner.Scan()
		ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
		retryOnly = ans == "y" || ans == "yes"
		if retryOnly {
			fmt.Printf("%s[+] Retry-only mode selected%s\n", Green, Reset)
		} else {
			// User declined retry-only: Clear session state for fresh start
			prevSession = nil
			os.Remove(sessionFile)
		}
	}

	delayBetweenRequests := time.Duration(config.DelaySeconds) * time.Second

	// Create results directory if not exists
	if err := os.MkdirAll("results", 0755); err != nil {
		fmt.Printf("Error creating results directory: %v\n", err)
		os.Exit(1)
	}

	var query, methodName, fullParam, queryBase64, firstPageQuery string
	var fullSearch bool
	var startPage, totalSize, totalPages int

	if retryOnly {
		// Reuse everything from the previous session — no new prompts, no size probe
		query = prevSession.Query
		fullSearch = prevSession.FullSearch
		methodName = prevSession.Method
		if fullSearch {
			fullParam = "&full=true"
		}
		queryBase64 = encodeBase64(query)
		totalSize = prevSession.TotalSize
		totalPages = prevSession.TotalPages
		firstPageQuery = query
		fmt.Printf("%s[+] Retry-only: query \"%s\" (%s) — %d page(s): %v%s\n\n",
			Green, query, methodName, len(prevSession.FailedPages), prevSession.FailedPages, Reset)
	} else {
		// Get query from user input (press enter to reuse the last query)
		if prevSession != nil {
			fmt.Printf("\nInput Query %s(enter to reuse: %s)%s: ", Yellow, prevSession.Query, Reset)
		} else {
			fmt.Print("\nInput Query: ")
		}
		scanner.Scan()
		query = strings.TrimSpace(scanner.Text())
		if query == "" && prevSession != nil {
			query = prevSession.Query
			fmt.Printf("%s[+] Reusing previous query%s\n", Green, Reset)
		}
		if query == "" {
			fmt.Println("Error: Query cannot be empty")
			os.Exit(1)
		}

		// Get search method from user input
		fmt.Println("\nSearch Method:")
		fmt.Println("  1. Standard (default)")
		fmt.Println("  2. Full")
		fmt.Print("Choose method [1/2] (default 1): ")
		scanner.Scan()
		methodChoice := strings.TrimSpace(scanner.Text())
		fullSearch = methodChoice == "2"
		methodName = "Standard"
		if fullSearch {
			fullParam = "&full=true"
			methodName = "Full"
		}
		fmt.Printf("%s[+] Search method: %s%s\n", Green, methodName, Reset)

		// Get start page (default 1) — allows resuming a previous/interrupted search
		suggestPage := 1
		if prevSession != nil && prevSession.Query == query && prevSession.FullSearch == fullSearch &&
			prevSession.LastPage < prevSession.TotalPages {
			suggestPage = prevSession.LastPage + 1
		}
		if suggestPage > 1 {
			fmt.Printf("\nStart page %s(default %d to resume previous search)%s: ", Yellow, suggestPage, Reset)
		} else {
			fmt.Print("\nStart page (default 1): ")
		}
		scanner.Scan()
		startPageInput := strings.TrimSpace(scanner.Text())
		startPage = suggestPage
		if startPageInput != "" {
			if val, err := strconv.Atoi(startPageInput); err == nil && val >= 1 {
				startPage = val
			} else {
				fmt.Printf("%s[!] Invalid start page, using %d%s\n", Yellow, startPage, Reset)
			}
		}
		fmt.Printf("%s[+] Starting from page: %d%s\n", Green, startPage, Reset)

		queryBase64 = encodeBase64(query)
		fmt.Printf("\nQuery (Base64): %s\n", queryBase64)

		fmt.Printf("\nConfiguration loaded:\n")
		fmt.Printf("- Delay: %v\n", delayBetweenRequests)
		fmt.Printf("- Results will be saved to: results/\n\n")

		// Probe the start page to determine total size
		fmt.Printf("Fetching page %d to determine total size...\n", startPage)
		firstPageURL := fmt.Sprintf("%s/api/v1/search/all?key=%s&page=%d&qbase64=%s%s",
			config.APIBase, config.APIKey, startPage, queryBase64, fullParam)
		firstPage, ferr := fetchFofaData(firstPageURL, delayBetweenRequests)
		if ferr != nil {
			fmt.Printf("%sError fetching first page: %v%s\n", Red, ferr, Reset)
			os.Exit(1)
		}
		if firstPage == nil {
			fmt.Printf("%sError: Received nil response from API%s\n", Red, Reset)
			os.Exit(1)
		}
		totalSize = firstPage.Size
		totalPages = int(math.Ceil(float64(totalSize) / 100.0))
		firstPageQuery = firstPage.Query
		if totalPages == 0 {
			fmt.Printf("%sError: API returned 0 total pages for query%s\n", Red, Reset)
			os.Exit(1)
		}

		fmt.Printf("Total size: %d\n", totalSize)
		fmt.Printf("Total pages to fetch: %d\n", totalPages)
		fmt.Printf("Query: %s\n\n", firstPageQuery)

		if startPage > totalPages {
			fmt.Printf("%s[!] Start page %d is beyond the total of %d pages. Nothing to fetch.%s\n",
				Red, startPage, totalPages, Reset)
			os.Exit(1)
		}
	}

	// Step 2: Fetch all pages sequentially — FOFA requires ordered pagination (error 820013)
	allResults := make([][]string, 0)

	// Determine output files — reuse the previous session's files when resuming/retrying
	var timestamp, domainFile, csvFile string
	reuseFiles := prevSession != nil &&
		(retryOnly ||
			(prevSession.Query == query && prevSession.FullSearch == fullSearch && startPage > 1)) &&
		fileExists(prevSession.DomainFile) &&
		fileExists(prevSession.CSVFile)

	if reuseFiles {
		timestamp = prevSession.Timestamp
		domainFile = prevSession.DomainFile
		csvFile = prevSession.CSVFile
		fmt.Printf("%s[+] Appending to existing files%s\n", Green, Reset)
	} else {
		timestamp = time.Now().Format("20060102_150405")
		domainFile = fmt.Sprintf("results/fofa_domain_%s.txt", timestamp)
		csvFile = fmt.Sprintf("results/fofa_results_%s.csv", timestamp)
		if err := createCSVWithHeader(csvFile); err != nil {
			fmt.Printf("Error creating CSV file: %v\n", err)
			os.Exit(1)
		}
	}

	// Build the list of pages to fetch
	var pagesToFetch []int
	if retryOnly {
		pagesToFetch = append(pagesToFetch, prevSession.FailedPages...)
	} else {
		for p := startPage; p <= totalPages; p++ {
			pagesToFetch = append(pagesToFetch, p)
		}
	}

	// Track the last successful page and any pages that failed, so nothing is lost
	lastSuccessPage := startPage - 1
	failedPages := make([]int, 0)
	if retryOnly {
		lastSuccessPage = prevSession.LastPage
	} else if reuseFiles {
		// Carry over earlier failed pages (before the resume point) so they get retried too
		for _, p := range prevSession.FailedPages {
			if p < startPage {
				failedPages = append(failedPages, p)
			}
		}
		if prevSession.LastPage > lastSuccessPage {
			lastSuccessPage = prevSession.LastPage
		}
	}

	// persist writes the current progress (last successful page + failed pages) to disk
	persist := func() {
		saveSessionState(sessionFile, &SessionState{
			Query:       query,
			Method:      methodName,
			FullSearch:  fullSearch,
			LastPage:    lastSuccessPage,
			TotalPages:  totalPages,
			TotalSize:   totalSize,
			FailedPages: failedPages,
			DomainFile:  domainFile,
			CSVFile:     csvFile,
			Timestamp:   timestamp,
			UpdatedAt:   time.Now().Format("2006-01-02 15:04:05"),
		})
	}

	if retryOnly {
		fmt.Printf("Retrying %d failed page(s): %v with %v delay...\n\n",
			len(pagesToFetch), pagesToFetch, delayBetweenRequests)
	} else {
		fmt.Printf("Starting to fetch pages %d-%d sequentially with %v delay...\n\n",
			startPage, totalPages, delayBetweenRequests)
	}

	var bar *StickyBar
	if retryOnly {
		bar = newStickyBar(len(pagesToFetch), totalSize)
	} else {
		bar = newStickyBar(totalPages, totalSize)
		bar.current = startPage - 1
	}
	bar.Init()

	const maxRetries = 3
	for _, page := range pagesToFetch {
		results, ok := fetchPageWithRetries(config, page, totalPages, queryBase64, fullParam,
			delayBetweenRequests, maxRetries, domainFile, csvFile, bar)
		if !ok {
			failedPages = append(failedPages, page)
			persist()
			continue
		}
		allResults = append(allResults, results...)
		if page > lastSuccessPage {
			lastSuccessPage = page
		}
		persist()
	}

	bar.Finish()

	// Automatic retry pass over pages that failed during a normal sweep
	// (skipped in retry-only mode — the sweep above already was the retry)
	if !retryOnly && len(failedPages) > 0 {
		fmt.Printf("\n%s[*] Retrying %d failed page(s): %v%s\n\n",
			Cyan+Bold, len(failedPages), failedPages, Reset)
		toRetry := failedPages
		failedPages = make([]int, 0)
		retryBar := newStickyBar(len(toRetry), totalSize)
		retryBar.Init()
		for _, page := range toRetry {
			results, ok := fetchPageWithRetries(config, page, totalPages, queryBase64, fullParam,
				delayBetweenRequests, maxRetries, domainFile, csvFile, retryBar)
			if !ok {
				failedPages = append(failedPages, page)
				persist()
				continue
			}
			allResults = append(allResults, results...)
			persist()
		}
		retryBar.Finish()
	}

	fmt.Printf("\n%s✓ All pages fetched! Total results: %d%s\n", Green+Bold, len(allResults), Reset)
	if len(failedPages) > 0 {
		fmt.Printf("%s[!] %d page(s) still failed: %v — rerun and resume to retry them%s\n",
			Yellow, len(failedPages), failedPages, Reset)
	}
	fmt.Println()

	// Step 3: Save summary JSON
	combined := CombinedResults{
		TotalSize:    totalSize,
		TotalPages:   totalPages,
		Query:        firstPageQuery,
		FetchedAt:    time.Now().Format("2006-01-02 15:04:05"),
		AllResults:   allResults,
		ResultsCount: len(allResults),
	}

	summaryFile := fmt.Sprintf("results/summary_%s.json", timestamp)
	err = saveToJSON(combined, summaryFile)
	if err != nil {
		fmt.Printf("%s[!] Error saving summary: %v%s\n", Yellow, err, Reset)
	} else {
		fmt.Printf("%s[+] Summary saved to %s%s\n", Green, summaryFile, Reset)
	}

	fmt.Printf("\n%s%s%s\n", Cyan+Bold, strings.Repeat("═", 60), Reset)
	fmt.Printf("%s[+] Scan selesai!%s\n", Green+Bold, Reset)
	fmt.Printf("%s[+] Domains  : results/fofa_domain_%s.txt%s\n", Green, timestamp, Reset)
	fmt.Printf("%s[+] CSV      : results/fofa_results_%s.csv%s\n", Green, timestamp, Reset)
	fmt.Printf("%s[+] Summary  : results/summary_%s.json%s\n", Green, timestamp, Reset)
	fmt.Printf("%s%s%s\n", Cyan+Bold, strings.Repeat("═", 60), Reset)
}

// fetchPageWithRetries fetches a single page (with retries) and saves its results
// to the domain/CSV files in real time. Returns the results and whether it succeeded.
func fetchPageWithRetries(config *Config, page, totalPages int, queryBase64, fullParam string,
	delay time.Duration, maxRetries int, domainFile, csvFile string, bar *StickyBar) ([][]string, bool) {

	apiURL := fmt.Sprintf("%s/api/v1/search/all?key=%s&page=%d&qbase64=%s%s",
		config.APIBase, config.APIKey, page, queryBase64, fullParam)

	var data *FofaResponse
	var fetchErr error
	var attempts int
	for attempt := 1; attempt <= maxRetries; attempt++ {
		attempts = attempt
		data, fetchErr = fetchFofaData(apiURL, delay)
		if fetchErr == nil {
			break
		}
		if attempt < maxRetries {
			time.Sleep(delay * 2)
		}
	}
	if fetchErr != nil {
		bar.Increment(fmt.Sprintf("%s❌ Page %d skipped after %d attempts: %v%s",
			Red, page, maxRetries, fetchErr, Reset))
		return nil, false
	}

	if err := appendDomains(data.Results, domainFile); err != nil {
		bar.Increment(fmt.Sprintf("%s⚠ Page %d: failed to save domains: %v%s",
			Yellow, page, err, Reset))
	}
	if err := appendToCSV(data.Results, csvFile); err != nil {
		bar.Increment(fmt.Sprintf("%s⚠ Page %d: failed to save CSV: %v%s",
			Yellow, page, err, Reset))
	}

	retryNote := ""
	if attempts > 1 {
		retryNote = fmt.Sprintf(" (retry x%d)", attempts-1)
	}
	bar.Increment(fmt.Sprintf("%s✓ Page %d/%d — %d results%s%s",
		Green, page, totalPages, len(data.Results), retryNote, Reset))
	return data.Results, true
}

// loadSessionState reads the persisted session state, if any
func loadSessionState(filename string) (*SessionState, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var s SessionState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Query == "" {
		return nil, fmt.Errorf("empty session")
	}
	return &s, nil
}

// saveSessionState writes the current progress to disk (best-effort)
func saveSessionState(filename string, s *SessionState) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filename, data, 0644)
}

// fileExists reports whether a regular file exists at the given path
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	return err == nil && !info.IsDir()
}

// loadConfig loads configuration from .env file
func loadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	config := &Config{
		APIBase:            "https://en.fofa.info",
		DelaySeconds:       2,
		ConcurrentRequests: 3,
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key=value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "FOFA_API_KEY":
			config.APIKey = value
		case "FOFA_API_BASE":
			config.APIBase = value
		case "DELAY_SECONDS":
			if val, err := strconv.Atoi(value); err == nil {
				config.DelaySeconds = val
			}
		case "CONCURRENT_REQUESTS":
			if val, err := strconv.Atoi(value); err == nil {
				config.ConcurrentRequests = val
			}
		}
	}

	if config.APIKey == "" {
		return nil, fmt.Errorf("FOFA_API_KEY not found in .env")
	}

	return config, scanner.Err()
}

// createDefaultEnv creates a default .env file
func createDefaultEnv() {
	content := `# FOFA API Configuration (by @willygoid)
# Get your API key from: https://fofa.info/userInfo

FOFA_API_KEY=your_api_key_here

# FOFA API Base URL (default: en.fofa.info)
# You can change to: fofa.info (CN), en.fofa.info (EN), etc.
FOFA_API_BASE=https://en.fofa.info

# Request configuration
DELAY_SECONDS=5
CONCURRENT_REQUESTS=1
`

	os.WriteFile(".env", []byte(content), 0644)
}

// fetchFofaData fetches data from FOFA API with browser-like headers
func fetchFofaData(url string, delay time.Duration) (*FofaResponse, error) {
	// Apply delay before request
	time.Sleep(delay)

	client := &http.Client{
		Timeout: 120 * time.Second,
	}

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add browser-like headers
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,id;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("DNT", "1")
	req.Header.Set("Referer", "https://fofa.info/")

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("rate limit exceeded (429) - increase delay time")
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status code %d. Response: %s", resp.StatusCode, string(bodyBytes))
	}

	// Handle gzip encoding
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var fofaResp FofaResponse
	err = json.Unmarshal(body, &fofaResp)
	if err != nil {
		respPreview := string(body)
		if len(respPreview) > 500 {
			respPreview = respPreview[:500] + "..."
		}
		return nil, fmt.Errorf("failed to parse JSON response: %w\nResponse: %s", err, respPreview)
	}

	if fofaResp.Error {
		tip := fofaResp.Tip
		if tip == "" {
			tip = "(no error message from API)"
		}
		// Show full response for debugging
		respStr := string(body)
		if len(respStr) > 300 {
			respStr = respStr[:300] + "..."
		}
		return nil, fmt.Errorf("API returned error: %s\nFull response: %s", tip, respStr)
	}

	if fofaResp.Size < 0 {
		return nil, fmt.Errorf("API returned invalid size: %d", fofaResp.Size)
	}

	return &fofaResp, nil
}

// saveToJSON saves the response to a JSON file
func saveToJSON(data interface{}, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	err = encoder.Encode(data)
	if err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	return nil
}

// createCSVWithHeader creates CSV file with header
func createCSVWithHeader(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString("URL,IP,Port\n")
	return err
}

// appendToCSV appends results to CSV file
func appendToCSV(results [][]string, filename string) error {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open CSV file: %w", err)
	}
	defer file.Close()

	for _, row := range results {
		if len(row) >= 3 {
			line := fmt.Sprintf("%s,%s,%s\n", row[0], row[1], row[2])
			if _, err := file.WriteString(line); err != nil {
				return err
			}
		}
	}

	return nil
}

// appendDomains appends only the first column (domains/URLs) to file
func appendDomains(results [][]string, filename string) error {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open domain file: %w", err)
	}
	defer file.Close()

	for _, row := range results {
		if len(row) >= 1 {
			if _, err := file.WriteString(row[0] + "\n"); err != nil {
				return err
			}
		}
	}

	return nil
}

// appendRawResults appends raw JSON results to file
func appendRawResults(results [][]string, filename string) error {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open result file: %w", err)
	}
	defer file.Close()

	// Convert each result row to JSON and append
	for _, row := range results {
		jsonData, err := json.Marshal(row)
		if err != nil {
			continue
		}
		if _, err := file.WriteString(string(jsonData) + "\n"); err != nil {
			return err
		}
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// encodeBase64 encodes string to base64
func encodeBase64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
