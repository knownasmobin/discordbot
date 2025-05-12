package youtube

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/kkdai/youtube/v2"
	"golang.org/x/net/proxy"
	"layeh.com/gopus"
)

// Client handles YouTube audio downloads and streaming
type Client struct {
	YoutubeClient youtube.Client
	CacheDir      string
	httpClient    *http.Client
	proxyList     []string
	lastUpdate    time.Time
	proxyMutex    sync.Mutex
	currentProxy  int              // Track current proxy index
	Invidious     *InvidiousClient // Invidious client for music playback
}

// createClientWithRandomizedHeaders creates an HTTP client with randomized headers
// to help bypass IP restrictions and fingerprinting
func createClientWithRandomizedHeaders() *http.Client {
	// Start with the default transport
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Disable keep-alives to prevent connection reuse
	transport.DisableKeepAlives = true

	// Set longer timeouts
	transport.TLSHandshakeTimeout = 15 * time.Second
	transport.ResponseHeaderTimeout = 15 * time.Second
	transport.ExpectContinueTimeout = 5 * time.Second

	// Create a client with the transport
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	// Wrap the client's Transport with a custom RoundTripper that adds random headers
	origTransport := client.Transport
	client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		// Common user agents
		userAgents := []string{
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.1.1 Safari/605.1.15",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:89.0) Gecko/20100101 Firefox/89.0",
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/90.0.4430.212 Safari/537.36",
		}

		// Set a random user agent
		req.Header.Set("User-Agent", userAgents[time.Now().UnixNano()%int64(len(userAgents))])

		// Add common headers to appear more like a browser
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Connection", "close") // Disable keep-alive at the HTTP level too
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Pragma", "no-cache")

		// Add a random referer
		referers := []string{
			"https://www.google.com/",
			"https://www.bing.com/",
			"https://search.yahoo.com/",
			"https://duckduckgo.com/",
		}
		req.Header.Set("Referer", referers[time.Now().UnixNano()%int64(len(referers))])

		return origTransport.RoundTrip(req)
	})

	return client
}

// createProxyEnabledClientWithProxy creates an HTTP client with a specific proxy
func createProxyEnabledClientWithProxy(proxyURL string) *http.Client {
	// Start with the default transport
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Parse the proxy URL
	parsedURL, err := url.Parse("http://" + proxyURL) // Add http:// prefix if not present
	if err != nil {
		fmt.Printf("Error parsing proxy URL: %v\n", err)
		return &http.Client{Transport: transport}
	}

	// Set the proxy
	transport.Proxy = http.ProxyURL(parsedURL)
	fmt.Printf("Using HTTP proxy: %s\n", proxyURL)

	return &http.Client{
		Transport: transport,
	}
}

// roundTripperFunc implements the RoundTripper interface
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// NewClient creates a new YouTube client
func NewClient() *Client {
	cacheDir := "./cache"
	// Create cache directory if it doesn't exist
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		os.Mkdir(cacheDir, 0755)
	}

	// Create an HTTP client with proxy support and randomized headers
	httpClient := createProxyEnabledClient()

	// Also create a client with randomized headers as a fallback
	randomizedClient := createClientWithRandomizedHeaders()

	// Create a YouTube client with the enhanced HTTP client
	youtubeClient := youtube.Client{
		HTTPClient: randomizedClient, // Use randomized headers by default
	}

	// Create an Invidious client for music playback
	invidiousClient := NewInvidiousClient()

	client := &Client{
		YoutubeClient: youtubeClient,
		CacheDir:      cacheDir,
		httpClient:    httpClient,
		Invidious:     invidiousClient,
	}

	// Initialize proxy list if proxy is configured
	if os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != "" || os.Getenv("SOCKS_PROXY") != "" {
		fmt.Println("🔄 Proxy configuration detected, initializing proxy list...")
		go client.updateProxyList()
		go client.startProxyUpdater()
	} else {
		fmt.Println("ℹ️ No proxy configuration detected, using direct connection with randomized headers")
	}

	return client
}

// createProxyEnabledClient creates an HTTP client with proxy support
func createProxyEnabledClient() *http.Client {
	// Start with the default transport
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Check for HTTP or HTTPS proxy
	httpProxy := os.Getenv("HTTP_PROXY")
	httpsProxy := os.Getenv("HTTPS_PROXY")
	socksProxy := os.Getenv("SOCKS_PROXY")
	proxyUser := os.Getenv("PROXY_USER")
	proxyPass := os.Getenv("PROXY_PASS")

	// Configure HTTP/HTTPS proxy if provided
	if httpProxy != "" || httpsProxy != "" {
		proxyURL := httpProxy
		if httpsProxy != "" {
			proxyURL = httpsProxy
		}

		if proxyURL != "" {
			// Add authentication if provided
			if proxyUser != "" && proxyPass != "" {
				proxyURL = strings.Replace(proxyURL, "://", fmt.Sprintf("://%s:%s@", proxyUser, proxyPass), 1)
			}

			if parsedURL, err := url.Parse(proxyURL); err == nil {
				transport.Proxy = http.ProxyURL(parsedURL)
				fmt.Printf("Using HTTP/HTTPS proxy: %s\n", proxyURL)
			} else {
				fmt.Printf("Error parsing proxy URL: %v\n", err)
			}
		}
	}

	// Configure SOCKS proxy if provided
	if socksProxy != "" {
		// Add authentication if provided
		if proxyUser != "" && proxyPass != "" {
			socksProxy = strings.Replace(socksProxy, "://", fmt.Sprintf("://%s:%s@", proxyUser, proxyPass), 1)
		}

		// Parse the SOCKS proxy URL
		socksURL, err := url.Parse(socksProxy)
		if err == nil {
			// Create a dialer that uses the SOCKS proxy
			dialer, err := proxy.FromURL(socksURL, proxy.Direct)
			if err == nil {
				// Override the dial function to use the SOCKS dialer
				transport.DialContext = dialer.(proxy.ContextDialer).DialContext
				fmt.Printf("Using SOCKS proxy: %s\n", socksProxy)
			} else {
				fmt.Printf("Error creating SOCKS dialer: %v\n", err)
			}
		} else {
			fmt.Printf("Error parsing SOCKS proxy URL: %v\n", err)
		}
	}

	// Return HTTP client with the configured transport
	return &http.Client{
		Transport: transport,
	}
}

// GetVideoID extracts the video ID from a YouTube URL
func (c *Client) GetVideoID(url string) (string, error) {
	// Regular expressions to match YouTube URL patterns
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`^https?://(?:www\.)?youtube\.com/watch\?v=([^&]+)`),
		regexp.MustCompile(`^https?://(?:www\.)?youtube\.com/embed/([^?]+)`),
		regexp.MustCompile(`^https?://(?:www\.)?youtube\.com/v/([^?]+)`),
		regexp.MustCompile(`^https?://(?:www\.)?youtu\.be/([^?]+)`),
		// Also match Invidious URLs
		regexp.MustCompile(`^https?://(?:www\.)?(?:invidious|yewtu\.be|invidio\.us|inv\.riverside\.rocks|invidio\.xamh\.de)/watch\?v=([^&]+)`),
	}

	for _, pattern := range patterns {
		if matches := pattern.FindStringSubmatch(url); len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", fmt.Errorf("invalid YouTube or Invidious URL: %s", url)
}

// SearchVideos searches for videos using Invidious
func (c *Client) SearchVideos(query string, limit int) ([]InvidiousSearchResult, error) {
	fmt.Printf("Searching for videos on Invidious: %s\n", query)

	// Use the Invidious client to search for videos
	results, err := c.Invidious.SearchVideos(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search videos on Invidious: %v", err)
	}

	return results, nil
}

// updateProxyList fetches and updates the list of HTTP proxies
func (c *Client) updateProxyList() error {
	c.proxyMutex.Lock()
	defer c.proxyMutex.Unlock()

	fmt.Println("🔄 Fetching new proxy list...")
	resp, err := http.Get("https://raw.githubusercontent.com/Vann-Dev/proxy-list/refs/heads/main/proxies/http-tested/youtube.txt")
	if err != nil {
		fmt.Printf("❌ Error fetching proxy list: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("❌ Error reading proxy list: %v\n", err)
		return err
	}

	// Split the content by spaces to get individual proxy URLs
	rawProxies := strings.Fields(string(body))
	fmt.Printf("📥 Found %d raw proxies\n", len(rawProxies))

	// Create a channel to receive working proxies
	workingProxyChan := make(chan string)
	done := make(chan bool)
	var workingProxies []string

	// Start testing proxies in parallel
	for _, proxy := range rawProxies {
		go func(p string) {
			if c.testProxy(p) {
				select {
				case workingProxyChan <- p:
					// Proxy sent successfully
				case <-done:
					// Testing is done
				}
			}
		}(proxy)
	}

	// Collect working proxies for 10 seconds
	go func() {
		for proxy := range workingProxyChan {
			workingProxies = append(workingProxies, proxy)
			fmt.Printf("📝 Added working proxy to list: %s\n", proxy)
		}
	}()

	// Wait for timeout
	<-time.After(10 * time.Second)
	close(done)
	close(workingProxyChan)

	// Update the proxy list
	c.proxyList = workingProxies
	c.lastUpdate = time.Now()
	fmt.Printf("✅ Proxy list updated with %d working proxies\n", len(workingProxies))
	if len(workingProxies) > 0 {
		fmt.Println("📋 Working proxies:")
		for _, proxy := range workingProxies {
			fmt.Printf("  - %s\n", proxy)
		}
	}
	return nil
}

// testProxy tests if a proxy can access YouTube
func (c *Client) testProxy(proxyURL string) bool {
	fmt.Printf("Testing proxy: %s\n", proxyURL)

	// Create a test client with the proxy
	client := createProxyEnabledClientWithProxy(proxyURL)

	// Set a timeout for the test
	client.Timeout = 5 * time.Second

	// Try to access YouTube's API
	resp, err := client.Get("https://www.youtube.com/oembed?url=https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	if err != nil {
		fmt.Printf("❌ Proxy %s failed: %v\n", proxyURL, err)
		return false
	}
	defer resp.Body.Close()

	// Check if we got a successful response
	if resp.StatusCode == http.StatusOK {
		fmt.Printf("✅ Proxy %s is working!\n", proxyURL)
		return true
	}

	fmt.Printf("❌ Proxy %s failed with status code: %d\n", proxyURL, resp.StatusCode)
	return false
}

// startProxyUpdater starts a goroutine to update the proxy list every 20 minutes
func (c *Client) startProxyUpdater() {
	ticker := time.NewTicker(20 * time.Minute)
	for range ticker.C {
		fmt.Println("Updating proxy list...")
		c.updateProxyList()
	}
}

// getNextProxy returns the next proxy from the list
func (c *Client) getNextProxy() string {
	c.proxyMutex.Lock()
	defer c.proxyMutex.Unlock()

	if len(c.proxyList) == 0 {
		return ""
	}

	// Get the current proxy and increment the index
	proxy := c.proxyList[c.currentProxy]
	c.currentProxy = (c.currentProxy + 1) % len(c.proxyList)
	return proxy
}

// convertToDiscordFormat converts audio to a format that Discord can play
// Outputs raw PCM data in the correct format for Opus encoding (s16le, 48kHz, 2 channels)
func (c *Client) convertToDiscordFormat(inFile, outFile string) error {
	fmt.Printf("Converting %s to %s\n", inFile, outFile)

	// Use ffmpeg to convert to raw PCM format that's compatible with Discord/Opus
	// -f s16le: 16-bit signed little-endian PCM format
	// -ar 48000: 48kHz sample rate (required by Discord)
	// -ac 2: 2 audio channels (stereo, required by Discord)
	cmd := exec.Command("ffmpeg",
		"-i", inFile,
		"-f", "s16le", // 16-bit signed little-endian PCM
		"-ar", "48000", // 48kHz sample rate (required by Discord)
		"-ac", "2", // 2 channels (stereo, required by Discord)
		outFile)

	// Capture ffmpeg output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		fmt.Printf("ffmpeg stdout:\n%s\n", stdout.String())
		fmt.Printf("ffmpeg stderr:\n%s\n", stderr.String())
		fmt.Printf("ffmpeg error: %v\n", err)
		return fmt.Errorf("ffmpeg conversion failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	// Log successful output
	if stdout.Len() > 0 {
		fmt.Printf("ffmpeg output:\n%s\n", stdout.String())
	}
	if stderr.Len() > 0 {
		fmt.Printf("ffmpeg warnings:\n%s\n", stderr.String())
	}

	return nil
}

// GetVideoInfo gets information about a YouTube video using Invidious
func (c *Client) GetVideoInfo(videoID string) (string, error) {
	fmt.Printf("Getting video info from Invidious for: %s\n", videoID)

	// Use the Invidious client to get video information
	video, err := c.Invidious.GetVideoInfo(videoID)
	if err != nil {
		return "", fmt.Errorf("failed to get video info from Invidious: %v", err)
	}

	return video.Title, nil
}

// DownloadAudio downloads audio from YouTube using only Invidious instances
func (c *Client) DownloadAudio(videoID string) (string, error) {
	fmt.Printf("Attempting to download video: %s\n", videoID)

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %v", err)
	}

	// Create temporary files
	tmpFile := filepath.Join(c.CacheDir, videoID+".tmp")
	cachePath := filepath.Join(c.CacheDir, videoID+".pcm")
	fmt.Printf("Temporary files:\n- Input: %s\n- Output: %s\n", tmpFile, cachePath)

	// Check if the file is already in cache
	if _, err := os.Stat(cachePath); err == nil {
		fmt.Printf("Found cached audio file: %s\n", cachePath)
		return cachePath, nil
	}

	// Get cookie file if available
	cookieFile := os.Getenv("YT_COOKIE_FILE")

	// Using only Invidious instances for music playback
	fmt.Printf("Using Invidious instances for download: %s\n", videoID)

	// Invidious instances to try
	invidiousInstances := []string{
		"https://invidious.snopyta.org",
		"https://yewtu.be",
		"https://invidious.kavin.rocks",
		"https://vid.puffyan.us",
		"https://inv.riverside.rocks",
		"https://invidio.xamh.de",
	}

	var lastError error
	for _, instance := range invidiousInstances {
		invidiousURL := fmt.Sprintf("%s/watch?v=%s", instance, videoID)
		fmt.Printf("Trying Invidious instance: %s\n", invidiousURL)

		// Prepare args for Invidious
		invidiousArgs := []string{
			"-f", "bestaudio",
			"-x", "--audio-format", "mp3",
			"-o", tmpFile,
			"--verbose",
			"--no-check-certificate",
			"--extractor-retries", "3",
			"--force-ipv4",
			"--ignore-errors",
			"--no-playlist",
			"--no-warnings",
		}

		// Add cookie file if available
		if cookieFile != "" {
			invidiousArgs = append(invidiousArgs, "--cookies", cookieFile)
			fmt.Printf("Using cookie file: %s\n", cookieFile)
		}

		// Add proxy options if available
		if os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != "" || os.Getenv("SOCKS_PROXY") != "" {
			fmt.Println("Using configured proxy for download")
			// yt-dlp will automatically use the system's proxy settings
		}

		// Add the Invidious URL
		invidiousArgs = append(invidiousArgs, invidiousURL)

		cmd := exec.Command("yt-dlp", invidiousArgs...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		cmdErr := cmd.Run()
		if cmdErr == nil {
			// Success! Convert to Discord format
			fmt.Printf("✅ Invidious download successful from %s, converting to Discord format...\n", instance)
			err := c.convertToDiscordFormat(tmpFile, cachePath)
			if err != nil {
				return "", fmt.Errorf("failed to convert audio: %v", err)
			}

			// Clean up the temporary file
			os.Remove(tmpFile)
			return cachePath, nil
		}

		fmt.Printf("Invidious instance %s failed: %v\n", instance, cmdErr)
		lastError = fmt.Errorf("invidious instance %s failed: %v", instance, cmdErr)
	}

	// If all Invidious instances failed, return the last error
	return "", fmt.Errorf("all Invidious instances failed: %v", lastError)
}

// Play plays YouTube audio in a Discord voice channel
func (c *Client) Play(vc *discordgo.VoiceConnection, url string) error {
	// Extract video ID
	videoID, err := c.GetVideoID(url)
	if err != nil {
		return fmt.Errorf("invalid YouTube or Invidious URL: %v", err)
	}

	// Check if cookie file is available
	cookieFile := os.Getenv("YT_COOKIE_FILE")
	if cookieFile == "" {
		fmt.Println("⚠️ YT_COOKIE_FILE not set. Using Invidious for all videos.")
	} else {
		// Verify cookie file exists
		if _, err := os.Stat(cookieFile); os.IsNotExist(err) {
			fmt.Printf("⚠️ Cookie file %s does not exist. Using Invidious instead.\n", cookieFile)
			// Reset cookie file since it doesn't exist
			os.Setenv("YT_COOKIE_FILE", "")
		} else {
			fmt.Printf("🍪 Using cookie file with Invidious: %s\n", cookieFile)
		}
	}

	// Try to download audio with enhanced retry logic
	var audioFile string
	maxRetries := 5 // Increased retries
	for i := 0; i < maxRetries; i++ {
		fmt.Printf("📥 Download attempt %d/%d for video ID: %s using Invidious\n", i+1, maxRetries, videoID)
		audioFile, err = c.DownloadAudio(videoID)
		if err == nil {
			break
		}

		// Log the error for debugging
		fmt.Printf("❌ Download attempt %d failed: %v\n", i+1, err)

		// For network errors, wait and retry
		if strings.Contains(err.Error(), "connection") ||
			strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "refused") {
			fmt.Println("🌐 Network error detected, waiting before retry...")
			time.Sleep(time.Duration(i+1) * time.Second) // Incremental backoff
			continue
		}

		// For other errors, continue to next attempt with a different Invidious instance
		fmt.Println("⚠️ Invidious instance failed, trying another instance...")
		time.Sleep(500 * time.Millisecond)
	}

	if err != nil {
		return fmt.Errorf("❌ failed to download audio after %d attempts: %v", maxRetries, err)
	}

	fmt.Printf("✅ Successfully downloaded audio for video ID: %s using Invidious\n", videoID)

	// Start speaking
	vc.Speaking(true)
	defer vc.Speaking(false)

	// Read the file
	file, err := os.Open(audioFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create a buffer for sending
	// Discord requires Opus-encoded audio, not raw PCM
	// We need to use the proper Discord audio packet size
	const frameSize = 960                // 20ms of audio at 48kHz
	pcmBuf := make([]int16, frameSize*2) // 2 channels

	// Create an Opus encoder
	opusEncoder, err := gopus.NewEncoder(48000, 2, gopus.Audio)
	if err != nil {
		return fmt.Errorf("failed to create Opus encoder: %v", err)
	}

	// Read the file as int16 PCM data
	reader := bufio.NewReader(file)
	for {
		// Read raw PCM data
		err = binary.Read(reader, binary.LittleEndian, pcmBuf)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return fmt.Errorf("error reading audio file: %v", err)
		}

		// Encode PCM to Opus
		opus, err := opusEncoder.Encode(pcmBuf, frameSize, frameSize*2*2)
		if err != nil {
			return fmt.Errorf("opus encoding error: %v", err)
		}

		// Send the Opus data
		vc.OpusSend <- opus
	}

	return nil
}
