// Package jikan reads MyAnimeList data through the Jikan API (api.jikan.moe).
package jikan

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Jikan allows about three requests a second; minGap keeps under that.
const (
	minGap      = 350 * time.Millisecond
	busyRetries = 3
)

// Client talks to Jikan. Its methods are safe for concurrent use.
type Client struct {
	HTTP *http.Client
	Base string

	mu   sync.Mutex
	next time.Time
}

// New returns a client for the public API.
func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{HTTP: httpClient, Base: "https://api.jikan.moe/v4"}
}

// Episode is what Jikan knows about one episode.
type Episode struct {
	Title         string `json:"title"`
	TitleRomaji   string `json:"title_romanji"`
	TitleJapanese string `json:"title_japanese"`
	Aired         string `json:"aired"`
	Synopsis      string `json:"synopsis"`
	Filler        bool   `json:"filler"`
	Recap         bool   `json:"recap"`
}

// Pictures returns every image URL Jikan lists for a show, in all sizes.
func (c *Client) Pictures(malID int) ([]string, error) {
	type sizes struct {
		Image string `json:"image_url"`
		Small string `json:"small_image_url"`
		Large string `json:"large_image_url"`
	}
	var answer struct {
		Data []struct {
			JPG  sizes `json:"jpg"`
			WebP sizes `json:"webp"`
		} `json:"data"`
	}
	if err := c.get(fmt.Sprintf("/anime/%d/pictures", malID), &answer); err != nil {
		return nil, err
	}
	var urls []string
	for _, picture := range answer.Data {
		for _, s := range []sizes{picture.JPG, picture.WebP} {
			for _, u := range []string{s.Image, s.Small, s.Large} {
				if u != "" {
					urls = append(urls, u)
				}
			}
		}
	}
	return urls, nil
}

// wait spaces requests out so the client stays under the rate limit.
func (c *Client) wait() {
	c.mu.Lock()
	now := time.Now()
	start := c.next
	if start.Before(now) {
		start = now
	}
	c.next = start.Add(minGap)
	c.mu.Unlock()
	time.Sleep(time.Until(start))
}

func (c *Client) get(path string, out any) error {
	for attempt := 0; ; attempt++ {
		c.wait()
		resp, err := c.HTTP.Get(c.Base + path)
		if err != nil {
			return fmt.Errorf("jikan: %w", err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("jikan: %w", err)
		}
		switch {
		case resp.StatusCode == http.StatusTooManyRequests && attempt < busyRetries:
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		case resp.StatusCode != http.StatusOK:
			return fmt.Errorf("jikan: status %d", resp.StatusCode)
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("jikan: %w", err)
		}
		return nil
	}
}
