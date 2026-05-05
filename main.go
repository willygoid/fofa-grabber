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
	fmt.Printf("\r\033[K%s FoFa API Grabber v1.1 by @willygoid | Total Size: %d %s\n",
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
		   FoFa API Grabber v1.1
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
	DelaySeconds       int
	ConcurrentRequests int
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

	// Get query from user input
	fmt.Print("Input Query: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	query := strings.TrimSpace(scanner.Text())

	if query == "" {
		fmt.Println("Error: Query cannot be empty")
		os.Exit(1)
	}

	// Convert query to base64
	queryBase64 := encodeBase64(query)
	fmt.Printf("\nQuery (Base64): %s\n", queryBase64)

	delayBetweenRequests := time.Duration(config.DelaySeconds) * time.Second

	// Create results directory if not exists
	if err := os.MkdirAll("results", 0755); err != nil {
		fmt.Printf("Error creating results directory: %v\n", err)
		os.Exit(1)
	}

	// Generate timestamp for this run
	timestamp := time.Now().Format("20060102_150405")

	fmt.Printf("\nConfiguration loaded:\n")
	fmt.Printf("- Delay: %v\n", delayBetweenRequests)
	fmt.Printf("- Results will be saved to: results/\n\n")

	// Step 1: Get first page to determine total size
	fmt.Println("Fetching first page to determine total size...")
	firstPageURL := fmt.Sprintf("https://fofa.info/api/v1/search/all?key=%s&page=1&qbase64=%s",
		config.APIKey, queryBase64)

	firstPage, err := fetchFofaData(firstPageURL, delayBetweenRequests)
	if err != nil {
		fmt.Printf("Error fetching first page: %v\n", err)
		os.Exit(1)
	}

	// Calculate total pages needed
	totalSize := firstPage.Size
	totalPages := int(math.Ceil(float64(totalSize) / 100.0))

	fmt.Printf("Total size: %d\n", totalSize)
	fmt.Printf("Total pages to fetch: %d\n", totalPages)
	fmt.Printf("Query: %s\n\n", firstPage.Query)

	// Step 2: Fetch all pages sequentially — FOFA requires ordered pagination (error 820013)
	allResults := make([][]string, 0)

	// File paths for real-time saving
	domainFile := fmt.Sprintf("results/fofa_domain_%s.txt", timestamp)
	csvFile := fmt.Sprintf("results/fofa_results_%s.csv", timestamp)

	// Create CSV file with header
	if err := createCSVWithHeader(csvFile); err != nil {
		fmt.Printf("Error creating CSV file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Starting to fetch %d pages sequentially with %v delay...\n\n",
		totalPages, delayBetweenRequests)

	bar := newStickyBar(totalPages, totalSize)
	bar.Init()

	const maxRetries = 3
	for page := 1; page <= totalPages; page++ {
		apiURL := fmt.Sprintf("https://fofa.info/api/v1/search/all?key=%s&page=%d&qbase64=%s",
			config.APIKey, page, queryBase64)

		var data *FofaResponse
		var fetchErr error
		var attempts int
		for attempt := 1; attempt <= maxRetries; attempt++ {
			attempts = attempt
			data, fetchErr = fetchFofaData(apiURL, delayBetweenRequests)
			if fetchErr == nil {
				break
			}
			if attempt < maxRetries {
				time.Sleep(delayBetweenRequests * 2)
			}
		}
		if fetchErr != nil {
			bar.Increment(fmt.Sprintf("%s❌ Page %d skipped after %d attempts: %v%s",
				Red, page, maxRetries, fetchErr, Reset))
			continue
		}

		if err := appendDomains(data.Results, domainFile); err != nil {
			bar.Increment(fmt.Sprintf("%s⚠ Page %d: failed to save domains: %v%s",
				Yellow, page, err, Reset))
		}
		if err := appendToCSV(data.Results, csvFile); err != nil {
			bar.Increment(fmt.Sprintf("%s⚠ Page %d: failed to save CSV: %v%s",
				Yellow, page, err, Reset))
		}

		allResults = append(allResults, data.Results...)
		retryNote := ""
		if attempts > 1 {
			retryNote = fmt.Sprintf(" (retry x%d)", attempts-1)
		}
		bar.Increment(fmt.Sprintf("%s✓ Page %d/%d — %d results%s%s",
			Green, page, totalPages, len(data.Results), retryNote, Reset))
	}

	bar.Finish()

	fmt.Printf("\n%s✓ All pages fetched! Total results: %d%s\n\n", Green+Bold, len(allResults), Reset)

	// Step 3: Save summary JSON
	combined := CombinedResults{
		TotalSize:    totalSize,
		TotalPages:   totalPages,
		Query:        firstPage.Query,
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

// loadConfig loads configuration from .env file
func loadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	config := &Config{
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

# Request configuration
DELAY_SECONDS=5
CONCURRENT_REQUESTS=1
`

	os.WriteFile(".env", []byte(content), 0644)
}

// fetchFofaData fetches data from FOFA API with browser-like headers
func fetchFofaData(url string, delay time.Duration) (*FofaResponse, error) {
	// Random delay untuk lebih natural
	time.Sleep(delay)

	client := &http.Client{
		Timeout: 30 * time.Second,
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
		return nil, fmt.Errorf("API returned status code: %d", resp.StatusCode)
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
		return nil, fmt.Errorf("failed to parse JSON: %w (response: %s)", err, string(body[:min(200, len(body))]))
	}

	if fofaResp.Error {
		return nil, fmt.Errorf("API returned error: %s", fofaResp.Tip)
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
