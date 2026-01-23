# FOFA Grabber

A powerful and efficient Go tool to fetch and save search results from FOFA (Cyberspace Search Engine) with real-time data saving, concurrent requests, and rate limiting support.

## Features
✨ **Interactive Query Input** - Enter your FOFA query directly when running the tool

🚀 **Concurrent Requests** - Configurable concurrent fetching for faster results

⏱️ **Rate Limiting** - Customizable delay between requests to avoid API rate limits

💾 **Real-time Saving** - Data is saved immediately as it's fetched (no data loss if interrupted)

📁 **Multiple Output Formats** - Domain list, JSON, CSV, and summary files

🔧 **Browser-like Headers** - Mimics real browser requests to avoid blocking

📊 **Progress Tracking** - Real-time progress display with percentage completion

🗂️ **Organized Output** - All results saved in timestamped files within `results/` folder

## Prerequisites

- Go 1.16 or higher
- FOFA API Key (get it from [FOFA User Info](https://fofa.info/userInfo))

## Installation

1. Clone this repository:
```bash
git clone https://github.com/willygoid/fofa-grabber.git
cd fofa-grabber
```

2. Run the program (it will create a default `.env` file):
```bash
go run main.go
```

3. Edit the `.env` file with your FOFA API key:
```env
FOFA_API_KEY=your_api_key_here
DELAY_SECONDS=5
CONCURRENT_REQUESTS=1
```

## Usage

### Basic Usage

```bash
go run main.go
```

When prompted, enter your FOFA query:
```
Input Query: app="Apache-Tomcat" && country="ID"
```

### Build Binary

To create a standalone executable:
```bash
go build -o fofa-grabber main.go
```

Then run:
```bash
./fofa-grabber
```

## Configuration

Edit the `.env` file to customize behavior:

| Variable | Description | Default |
|----------|-------------|---------|
| `FOFA_API_KEY` | Your FOFA API key | - |
| `DELAY_SECONDS` | Delay between requests (seconds) | 2 |
| `CONCURRENT_REQUESTS` | Number of concurrent requests | 3 |

### Recommended Settings

- **Safe/Slow**: `DELAY_SECONDS=3-5`, `CONCURRENT_REQUESTS=1-2`
- **Normal**: `DELAY_SECONDS=2`, `CONCURRENT_REQUESTS=3`
- **Fast** (risky): `DELAY_SECONDS=1`, `CONCURRENT_REQUESTS=5`

⚠️ **Warning**: Too aggressive settings may trigger rate limiting (HTTP 429)

## Query Examples

FOFA uses a powerful query syntax. Here are some examples:

```bash
# Search for Livewire applications in Indonesia
(body="wire:id" || body="wire:click=") && country="ID"

# Search for Apache Tomcat servers
app="Apache-Tomcat" && country="ID"

# Search for specific ports
port="8080" && country="ID"

# Search by IP range
ip="103.0.0.0/8" && country="ID"

# Search by domain
domain="example.com"

# Combine multiple conditions
app="nginx" && port="443" && country="ID"
```

For more query syntax, visit [FOFA Query Syntax](https://fofa.info/api).

## Output Files

All files are saved in the `results/` directory with timestamps:

```
results/
├── fofa_domain_20260123_143045.txt    # List of domains/URLs (one per line)
├── fofa_json_20260123_143045.txt      # Raw JSON results (one per line)
├── fofa_results_20260123_143045.csv   # CSV format (URL, IP, Port)
└── summary_20260123_143045.json       # Complete summary with metadata
```

### File Descriptions

- **fofa_domain_*.txt**: Clean list of all discovered domains/URLs
- **fofa_json_*.txt**: Raw JSON data for each result
- **fofa_results_*.csv**: Easy-to-process CSV format
- **summary_*.json**: Complete data with query info and timestamps

## Example Output

```
Configuration loaded:
- Delay: 2s
- Concurrent requests: 3
- Results will be saved to: results/

Input Query: app="Apache-Tomcat" && country="ID"

Query (Base64): YXBwPSJBcGFjaGUtVG9tY2F0IiAmJiBjb3VudHJ5PSJJRCI=

Fetching first page to determine total size...
Total size: 1500
Total pages to fetch: 15
Query: app="Apache-Tomcat" && country="ID"

Starting to fetch 15 pages with 3 concurrent requests and 2s delay...

✓ Progress: 3/15 pages completed (20.0%) - 100 results saved
✓ Progress: 6/15 pages completed (40.0%) - 100 results saved
✓ Progress: 9/15 pages completed (60.0%) - 100 results saved
✓ Progress: 12/15 pages completed (80.0%) - 100 results saved
✓ Progress: 15/15 pages completed (100.0%) - 100 results saved

✓ All pages fetched successfully!
Total results collected: 1500

✓ Summary saved to results/summary_20260123_143045.json

All files saved in results/ directory:
  - fofa_domain_20260123_143045.txt (all domains)
  - fofa_json_20260123_143045.txt (raw JSON results)
  - fofa_results_20260123_143045.csv (CSV format)
  - summary_20260123_143045.json (complete summary)
```

## Error Handling

### Rate Limiting (HTTP 429)

If you encounter rate limiting:
```
❌ Error fetching page X: rate limit exceeded (429) - increase delay time
```

**Solution**: Increase `DELAY_SECONDS` in `.env` or reduce `CONCURRENT_REQUESTS`

### Invalid API Key

```
Error fetching first page: API returned error: Invalid API key
```

**Solution**: Check your `FOFA_API_KEY` in `.env`

### Empty Query

```
Error: Query cannot be empty
```

**Solution**: Enter a valid FOFA query when prompted

## Advanced Features

### Real-time Saving

Data is saved immediately as it's fetched. If the program is interrupted:
- All fetched data is already saved
- You can safely interrupt (Ctrl+C) and still have partial results
- Resume by running again with a different query

### Thread-Safe Operations

- Uses mutex locks for concurrent file writing
- Safe for high concurrent request settings
- No data corruption or race conditions

### GZIP Support

Automatically handles GZIP-compressed API responses for efficient data transfer.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Disclaimer

This tool is for educational and research purposes only. Always:
- Respect FOFA's Terms of Service
- Use appropriate rate limiting
- Don't overload the API
- Use the data responsibly

## Author

Willy The Great - [@willygoid](https://github.com/willygoid)

## Acknowledgments

- [FOFA](https://fofa.info) - Cyberspace Search Engine
- Go community for excellent libraries

## Support

If you find this tool useful, please give it a ⭐ on GitHub!

For issues or questions, please [open an issue](https://github.com/willygoid/fofa-grabber/issues).
