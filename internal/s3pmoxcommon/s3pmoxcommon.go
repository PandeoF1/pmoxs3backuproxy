package s3pmoxcommon

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"tizbac/pmoxs3backuproxy/internal/s3backuplog"

	"github.com/minio/minio-go/v7"
)

var (
	Collect *Collector
	once    sync.Once
)

type Collector struct {
	mu              sync.RWMutex
	started         bool
	Client          *minio.Client
	snapshotSizes   map[string]int64
	bucketSizes     map[string]int64
	refreshInterval time.Duration
	stopChan        chan struct{}
	snapshotPrefix  string
}

func NewSizeCollection(refreshInterval time.Duration, snapshotPrefix string) *Collector {
	once.Do(func() {
		Collect = &Collector{
			Client:          nil,
			snapshotSizes:   make(map[string]int64),
			bucketSizes:     make(map[string]int64),
			refreshInterval: refreshInterval,
			snapshotPrefix:  snapshotPrefix,
			stopChan:        make(chan struct{}),
		}
		Collect.Start()
	})
	return Collect
}

func (sc *Collector) Start() {
	go func() {
		ticker := time.NewTicker(sc.refreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if sc.Client != nil {
					sc.updateSizes()
					sc.Display()
				} else {
					s3backuplog.ErrorPrint("No client found")
				}
			case <-sc.stopChan:
				return
			}
		}
	}()
}

func (sc *Collector) Display() {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	for k, v := range sc.bucketSizes {
		s3backuplog.DebugPrint("Bucket: %s size: %d", k, v)
	}
	for k, v := range sc.snapshotSizes {
		s3backuplog.DebugPrint("Snapshot: %s size: %d", k, v)
	}
}

func (sc *Collector) Stop() {
	close(sc.stopChan)
}

func (sc *Collector) GetBucketSize(bucketName string) int64 {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.bucketSizes[bucketName]
}

func (sc *Collector) GetSnapshotSize(snapshotPrefix string) int64 {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.snapshotSizes[snapshotPrefix]
}

func (sc *Collector) AddBucketToMonitor(bucketName string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.snapshotSizes[bucketName] = 0
}

func (sc *Collector) updateSizes() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Reset les maps
	sc.bucketSizes = make(map[string]int64)
	sc.snapshotSizes = make(map[string]int64)

	ctx := context.Background()

	// Collecter les tailles des buckets
	buckets, err := sc.Client.ListBuckets(ctx)
	if err != nil {
		log.Printf("Erreur lors de la récupération de la liste des buckets: %v", err)
		return
	}

	for _, bucket := range buckets {
		var bucketTotalSize int64
		objectCh := sc.Client.ListObjects(ctx, bucket.Name, minio.ListObjectsOptions{
			Recursive: true,
		})

		for object := range objectCh {
			if object.Err != nil {
				log.Printf("Erreur lors de la récupération des objets du bucket %s: %v", bucket.Name, object.Err)
				continue
			}
			bucketTotalSize += object.Size
		}

		s3backuplog.DebugPrint("Bucket: %s size: %d", bucket.Name, bucketTotalSize)
		sc.bucketSizes[bucket.Name] = bucketTotalSize
	}

	// Collecter les tailles des snapshots
	for _, bucket := range buckets {
		var snapshotTotalSize int64
		objectCh := sc.Client.ListObjects(ctx, bucket.Name, minio.ListObjectsOptions{
			Recursive: true,
			Prefix:    sc.snapshotPrefix,
		})

		for object := range objectCh {
			if object.Err != nil {
				log.Printf("Erreur lors de la récupération des snapshots: %v", object.Err)
				continue
			}

			// Collecter la taille totale des snapshots
			s3backuplog.DebugPrint("Snapshot: %s size: %d", object.Key, object.Size)
			snapshotTotalSize += object.Size

			// Optionnel : collecter la taille de chaque snapshot individuellement
			sc.snapshotSizes[object.Key] = object.Size
		}

		// Ajouter la taille totale des snapshots pour chaque bucket
		sc.snapshotSizes[bucket.Name+"/snapshots"] = snapshotTotalSize
	}
}

// This function would ideally return the max size of the bucket.
// Since MinIO doesn't expose this directly, you may want to configure or track this yourself.
func GetBucketMaxSize() int64 {
	// Return the max size of the bucket, which could be set via a configuration.
	// For example, if you're using a specific policy, set that size here.
	// For example, let's assume a max size of 1TB (1 TB = 1,099,511,627,776 bytes)
	return 1099511627776 // 1 TB in bytes
}

func ListSnapshots(c minio.Client, datastore string, returnCorrupted bool) ([]Snapshot, error) {
	resparray := make([]Snapshot, 0)
	resparray2 := make([]Snapshot, 0)
	prefixMap := make(map[string]*Snapshot)
	ctx := context.Background()
	for object := range c.ListObjects(
		ctx, datastore,
		minio.ListObjectsOptions{Recursive: true, Prefix: "backups/",
			WithMetadata: true,
		}) {
		//The object name is backupid|unixtimestamp|type
		path := strings.Split(object.Key, "/")
		if strings.Count(object.Key, "/") == 2 {
			fields := strings.Split(path[1], "|")
			existing_S, ok := prefixMap[path[1]]
			if ok {
				if len(path) == 3 {
					/** Dont add the custom chunk index list
					 * for dynamic backup to filelist
					 **/
					if strings.HasSuffix(path[2], ".csjson") {
						continue
					}
					if object.UserTags["protected"] == "true" {
						existing_S.Protected = true
					}
					if object.UserTags["note"] != "" {
						note, _ := base64.RawStdEncoding.DecodeString(object.UserTags["note"])
						existing_S.Comment = string(note)
					}
					existing_S.Files = append(existing_S.Files, SnapshotFile{
						Filename:  path[2],
						CryptMode: "none", //TODO
						Size:      uint64(object.Size),
					})
				}
				continue
			}

			backupid := fields[0]
			backuptime := fields[1]
			backuptype := fields[2]
			backuptimei, _ := strconv.ParseUint(backuptime, 10, 64)
			S := Snapshot{
				BackupID:   backupid,
				BackupTime: backuptimei,
				BackupType: backuptype,
				Files:      make([]SnapshotFile, 0),
				Datastore:  datastore,
				corrupted:  false,
			}

			if len(path) == 3 {
				S.Files = append(S.Files, SnapshotFile{
					Filename:  path[2],
					CryptMode: "none", //TODO
					Size:      uint64(object.Size),
				})
			}

			resparray = append(resparray, S)
			prefixMap[path[1]] = &resparray[len(resparray)-1]

		}

		if strings.HasSuffix(object.Key, "/corrupted") {
			prefixMap[path[1]].corrupted = true
		}
	}
	for _, s := range resparray {
		if returnCorrupted || !s.corrupted {
			resparray2 = append(resparray2, s)
		}
	}
	return resparray2, ctx.Err()
}

func GetLatestSnapshot(c minio.Client, ds string, id string, time uint64) (*Snapshot, error) {
	snapshots, err := ListSnapshots(c, ds, false)
	if err != nil {
		s3backuplog.ErrorPrint(err.Error())
		return nil, err
	}

	if len(snapshots) == 0 {
		return nil, nil
	}

	var mostRecent = &Snapshot{}
	mostRecent = nil
	for _, sl := range snapshots {
		if sl.BackupTime == time && sl.BackupID == id {
			s3backuplog.DebugPrint("GetLatestSnapshot: ignoring currently processed snapshot.")
			continue
		}
		if (mostRecent == nil || sl.BackupTime > mostRecent.BackupTime) && id == sl.BackupID {
			mostRecent = &sl
		}
	}

	if mostRecent == nil {
		return nil, nil
	}

	return mostRecent, nil
}

func (S *Snapshot) InitWithQuery(v url.Values) {
	S.BackupID = v.Get("backup-id")
	S.BackupTime, _ = strconv.ParseUint(v.Get("backup-time"), 10, 64)
	S.BackupType = v.Get("backup-type")
}

func (S *Snapshot) InitWithForm(r *http.Request) {
	S.BackupID = r.FormValue("backup-id")
	S.BackupTime, _ = strconv.ParseUint(r.FormValue("backup-time"), 10, 64)
	S.BackupType = r.FormValue("backup-type")
}

func (S *Snapshot) S3Prefix() string {
	return fmt.Sprintf("backups/%s|%d|%s", S.BackupID, S.BackupTime, S.BackupType)
}

func (S *Snapshot) GetFiles(c minio.Client) {
	for object := range c.ListObjects(
		context.Background(), S.Datastore,
		minio.ListObjectsOptions{Recursive: true, Prefix: S.S3Prefix()},
	) {
		file := SnapshotFile{}
		path := strings.Split(object.Key, "/")
		file.Filename = path[2]
		file.Size = uint64(object.Size)
		S.Files = append(S.Files, file)
	}
}

func (S *Snapshot) ReadTags(c minio.Client) (map[string]string, error) {
	existingTags, err := c.GetObjectTagging(
		context.Background(),
		S.Datastore,
		S.S3Prefix()+"/index.json.blob",
		minio.GetObjectTaggingOptions{},
	)
	if err != nil {
		s3backuplog.ErrorPrint("Unable to get tags: %s", err.Error())
		return nil, err
	}
	return existingTags.ToMap(), nil
}

func (S *Snapshot) Delete(c minio.Client) error {
	objectsCh := make(chan minio.ObjectInfo)
	go func() {
		defer close(objectsCh)
		// List all objects from a bucket-name with a matching prefix.
		opts := minio.ListObjectsOptions{Prefix: S.S3Prefix(), Recursive: true}
		for object := range c.ListObjects(context.Background(), S.Datastore, opts) {
			if object.Err != nil {
				s3backuplog.ErrorPrint(object.Err.Error())
			}
			objectsCh <- object
		}
	}()
	errorCh := c.RemoveObjects(context.Background(), S.Datastore, objectsCh, minio.RemoveObjectsOptions{})
	for e := range errorCh {
		s3backuplog.ErrorPrint("Failed to remove " + e.ObjectName + ", error: " + e.Err.Error())
		return e.Err
	}
	return nil
}

func GetLookupType(Typeflag string) minio.BucketLookupType {
	switch Typeflag {
	case "path":
		return minio.BucketLookupPath
	case "dns":
		return minio.BucketLookupDNS
	default:
		return minio.BucketLookupAuto
	}
}
