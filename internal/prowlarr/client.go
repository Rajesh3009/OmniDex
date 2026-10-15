package prowlarr

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"local-indexer/internal/db"
)

type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type SearchResultItem struct {
	Guid        string    `json:"guid"`
	Title       string    `json:"title"`
	Size        int64     `json:"size"`
	InfoHash    string    `json:"infoHash"`
	Indexer     string    `json:"indexer"`
	Seeders     int       `json:"seeders"`
	Leechers    int       `json:"leechers"`
	PublishDate time.Time `json:"publishDate"`
	MagnetURL   string    `json:"magnetUrl"`
	DownloadURL string    `json:"downloadUrl"`
	Categories  []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"categories"`
}

func NewClient(baseURL, apiKey string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Client) Search(query string, limit int) ([]SearchResultItem, error) {
	if c.BaseURL == "" || c.APIKey == "" {
		return nil, fmt.Errorf("prowlarr base URL or API key not configured")
	}

	reqURL, err := url.Parse(fmt.Sprintf("%s/api/v1/search", c.BaseURL))
	if err != nil {
		return nil, err
	}

	q := reqURL.Query()
	q.Set("query", query)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequest("GET", reqURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prowlarr request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("prowlarr returned status %d: %s", resp.StatusCode, string(body))
	}

	var results []SearchResultItem
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode prowlarr results: %w", err)
	}

	return results, nil
}

// ConvertToTorrentRecord converts a Prowlarr search result to a local TorrentRecord
func (item *SearchResultItem) ToRecord() db.TorrentRecord {
	category := "Other"
	if len(item.Categories) > 0 {
		category = item.Categories[0].Name
	}

	magnet := item.MagnetURL
	hash := strings.ToLower(item.InfoHash)
	if hash == "" && strings.Contains(magnet, "btih:") {
		// extract from magnet
		parts := strings.Split(magnet, "btih:")
		if len(parts) > 1 {
			hash = strings.Split(parts[1], "&")[0]
		}
	}
	if magnet == "" && hash != "" {
		magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hash, url.QueryEscape(item.Title))
	}

	return db.TorrentRecord{
		InfoHash:    hash,
		Name:        item.Title,
		Size:        item.Size,
		FileCount:   1,
		CreatedTime: item.PublishDate,
		Source:      "prowlarr",
		Seeders:     item.Seeders,
		Leechers:    item.Leechers,
		Category:    category,
		MagnetURI:   magnet,
	}
}
