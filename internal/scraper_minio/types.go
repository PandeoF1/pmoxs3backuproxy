package scraperminio

import (
	"net/http"
	"time"
)

type ScraperMinio struct {
	login    Login
	token    string
	endpoint string
	client   *http.Client
	data     *DataStoreStatus
}

type Login struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

type DataStoreStatus struct {
	Avail   int64 `json:"avail"`
	Total   int64 `json:"total"`
	Used    int64 `json:"used"`
	Counts  int64 `json:"counts"`
	GCState bool  `json:"gc-status"`
}

type Info struct {
	AdvancedMetricsStatus string `json:"advancedMetricsStatus"`
	Backend               struct {
		BackendType      string `json:"backendType"`
		OnlineDrives     int    `json:"onlineDrives"`
		RrSCParity       int    `json:"rrSCParity"`
		StandardSCParity int    `json:"standardSCParity"`
	} `json:"backend"`
	Buckets int `json:"buckets"`
	Objects int `json:"objects"`
	Servers []struct {
		CommitID string `json:"commitID"`
		Drives   []struct {
			AvailableSpace int64  `json:"availableSpace"`
			DrivePath      string `json:"drivePath"`
			Endpoint       string `json:"endpoint"`
			State          string `json:"state"`
			TotalSpace     int64  `json:"totalSpace"`
			UsedSpace      int64  `json:"usedSpace"`
			UUID           string `json:"uuid"`
		} `json:"drives"`
		Endpoint string `json:"endpoint"`
		Network  struct {
			One271119000 string `json:"127.1.1.1:9000"`
		} `json:"network"`
		PoolNumber int       `json:"poolNumber"`
		State      string    `json:"state"`
		Uptime     int       `json:"uptime"`
		Version    time.Time `json:"version"`
	} `json:"servers"`
	Usage   int64       `json:"usage"`
	Widgets interface{} `json:"widgets"`
}

type Bucket struct {
	Buckets []struct {
		CreationDate time.Time `json:"creation_date"`
		Details      struct {
			Quota struct {
			} `json:"quota"`
		} `json:"details"`
		Name     string `json:"name"`
		Objects  int    `json:"objects"`
		RwAccess struct {
			Read  bool `json:"read"`
			Write bool `json:"write"`
		} `json:"rw_access"`
		Size int64 `json:"size"`
	} `json:"buckets"`
	Total int `json:"total"`
}
