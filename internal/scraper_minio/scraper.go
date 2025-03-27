package scraperminio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func NewScraperMinio(accessKey, secretKey, endpoint string) *ScraperMinio {
	return &ScraperMinio{
		login: Login{
			AccessKey: accessKey,
			SecretKey: secretKey,
		},
		endpoint: endpoint,
		client:   &http.Client{},
		data:     &DataStoreStatus{},
	}
}

func extractTokenFromHeader(resp *http.Response) string {
	cookies := resp.Header["Set-Cookie"]

	for _, cookieStr := range cookies {
		parts := strings.Split(cookieStr, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "token=") {
				return strings.TrimPrefix(part, "token=")
			}
		}
	}
	return ""
}

func (s *ScraperMinio) req_login() error {
	url := fmt.Sprintf("http://%s/api/v1/login", s.endpoint)
	body, err := json.Marshal(s.login)
	if err != nil {
		return fmt.Errorf("failed to marshal login data: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		return fmt.Errorf("login failed with status: %s", resp.Status)
	}

	s.token = extractTokenFromHeader(resp)
	if s.token == "" {
		return fmt.Errorf("no token received")
	}
	return nil
}

func (s *ScraperMinio) req_admin_info() ([]byte, error) {
	url := fmt.Sprintf("http://%s/api/v1/admin/info", s.endpoint)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin info request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	cookie := &http.Cookie{
		Name:     "token",
		Value:    s.token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
	}
	req.AddCookie(cookie)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("admin info request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to get admin info with status: %s", resp.Status)
	}
	body, _ := io.ReadAll(resp.Body)
	return body, nil
}

func (s *ScraperMinio) ScrapBucketData() (DataStoreStatus, error) {
	datastore := DataStoreStatus{}

	// Login Request
	err := s.req_login()
	if err != nil {
		return datastore, fmt.Errorf("login failed: %v", err)
	}

	// Get Admin Info
	resp, err := s.req_admin_info()
	var info Info
	err = json.Unmarshal(resp, &info)
	if err != nil {
		return datastore, fmt.Errorf("failed to decode admin info: %v", err)
	}

	// Calculate total, used, and available space
	var totalSpace, usedSpace int64
	for _, server := range info.Servers {
		for _, drive := range server.Drives {
			totalSpace += drive.TotalSpace
			usedSpace += drive.UsedSpace
		}
	}

	// Populate DataStoreStatus
	datastore.Total = totalSpace
	datastore.Used = usedSpace
	datastore.Avail = totalSpace - usedSpace
	datastore.Counts = int64(info.Buckets)

	// Determine GC state (this might need to be adjusted based on your specific requirements)
	datastore.GCState = info.AdvancedMetricsStatus == "enabled"

	return datastore, nil
}
