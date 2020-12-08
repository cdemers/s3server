package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/gob"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"runtime/debug"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"

	"github.com/coocood/freecache"
	"github.com/docker/go-units"
	"gopkg.in/alecthomas/kingpin.v2"

	log "github.com/sirupsen/logrus"
	stash "gopkg.in/stash.v1"
)

var (
	version = "0.3.1"

	debugMode              = kingpin.Flag("debug", "Debug mode.").Envar("DEBUG").Bool()
	heartbeatRoute         = kingpin.Flag("heartbeat-route", "The HTTP route of the heartbeat check, include the slashes when necessary  (env: HEARTBEAT_ROUTE).").Default("/health").Envar("HEARTBEAT_ROUTE").String()
	s3Bucket               = kingpin.Flag("s3-bucket", "The AWS S3 Bucket name (env: S3_BUCKET).").PlaceHolder("S3_BUCKET").Required().Envar("S3_BUCKET").String()
	maxRamCacheSize        = kingpin.Flag("ram-cache-size", "The RAM cache size in human format, ex \"300 MB\". The memory is pre-allocated at startup time. (env: RAM_CACHE_SIZE).").Default("300 MB").Envar("RAM_CACHE_SIZE").String()
	maxDiskCacheItemSize   = kingpin.Flag("disk-cache-item-size", "The disk cache maximum object size, in human format, ex \"50 MB\" (env: DISK_CACHE_OBJECT_SIZE).").Default("50 MB").Envar("DISK_CACHE_OBJECT_SIZE").String()
	maxDiskCacheItemNumber = kingpin.Flag("disk-cache-item-number", "The maximum number of disk cache objects, ex \"20\" (env: DISK_CACHE_OBJECT_NUMBER).").Default("20").Envar("DISK_CACHE_OBJECT_NUMBER").Int64()
	diskCacheFolder        = kingpin.Flag("disk-cache-path", "The path of the disk cache folder, enough space must be available for the configured disk cache (env: DISK_CACHE_PATH).").Default("/tmp").Envar("DISK_CACHE_PATH").String()
	// For exapmle, if you set a maximum number of disk cache objects to 20, and
	// each object has a maximum size of 50 MB, then the maximum size of the cache
	// on disk will be 1GB (20 x 50 MB).

	ramCache  *freecache.Cache
	diskCache *stash.Cache
)

// Media can be used to keep structured data about a media and the media
// binary content itself.  Having it contained in one struc simplifies the
// serialization process.
type Media struct {
	Key         []byte
	ContentType string
	ETag        string
	Body        []byte
}

func handler(w http.ResponseWriter, r *http.Request) {
	s3Key := r.URL.EscapedPath()
	requestURI := r.URL.RequestURI()

	hasher := sha1.New()
	io.WriteString(hasher, requestURI)
	key := hasher.Sum(nil)
	keystr := fmt.Sprintf("%x", key)
	log.Debugf("hashed key (%s): %s", requestURI, keystr)

	value, err := ramCache.Get(key)
	if err != nil {
		if err == freecache.ErrNotFound {
			log.Debugf("key %s not found in RAM cache", keystr)

			valueFromDisk, err := diskCache.Get(keystr)
			if err != nil {
				log.Debugf("key %s not found in disk cache", keystr)
			} else {

				value, err := ioutil.ReadAll(valueFromDisk)
				if err != nil {
					log.Debug(err)
				}

				log.Debugf("disk cache hit, first six bytes of value are: 0x%x\n", value[0:6])

				decBuf := bytes.NewBuffer(value)
				media := Media{}
				err = gob.NewDecoder(decBuf).Decode(&media)
				if err != nil {
					log.Error(err)
				} else {
					w.Header().Set("ContentType", media.ContentType)
					w.Header().Set("ETag", media.ETag)
					w.Write(media.Body)
					return
				}

			}

		} else {
			log.Debugf("RAM cache error: %s", err)
		}
	} else {
		log.Debugf("RAM cache hit, first six bytes of value are: 0x%x\n", value[0:6])

		decBuf := bytes.NewBuffer(value)
		media := Media{}
		err = gob.NewDecoder(decBuf).Decode(&media)
		if err != nil {
			log.Error(err)
		} else {
			w.Header().Set("ContentType", media.ContentType)
			w.Header().Set("ETag", media.ETag)
			w.Write(media.Body)
			return
		}

	}

	config := &aws.Config{
		Region: aws.String("us-east-1"),
	}

	svc := s3.New(session.New(config))
	input := &s3.GetObjectInput{
		Bucket: aws.String(*s3Bucket),
		Key:    aws.String(s3Key),
	}

	result, err := svc.GetObject(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			log.Error(aerr.Error())
			switch aerr.Code() {
			case s3.ErrCodeNoSuchKey:
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("404 - File Not Found"))
				return
			default:
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("500 - Internal Server Error"))
				return
			}
		} else {
			log.Error(err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 - Internal Server Error"))
			return
		}

		// return
	}

	log.Debug("value of r.RequestURI: ", r.RequestURI)
	log.Debug("value of r.URL.EscapedPath(): ", r.URL.EscapedPath())

	if result.ContentType != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("500 - Internal server error"))
		log.Debug("File has no valid ContentType")
		return
	}

	if *result.ContentType == "application/x-directory" {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 - File Not Found"))
		return
	}

	media := Media{
		Key:         key,
		ContentType: *result.ContentType,
	}

	if result.ETag != nil {
		media.ETag = *result.ETag
	}

	// fmt.Printf("%#v\n", result)

	media.Body, err = ioutil.ReadAll(result.Body)
	if err != nil {
		log.Debug(err)
	}

	encBuf := new(bytes.Buffer)
	err = gob.NewEncoder(encBuf).Encode(media)
	if err != nil {
		log.Debug(err)
	}

	log.Debug("value encoded size: ", units.HumanSize(float64(encBuf.Len())))

	err = ramCache.Set(key, encBuf.Bytes(), 0)
	if err != nil {
		log.Debug("will not cache in ram: ", err)

		keystr := fmt.Sprintf("%x", key)
		log.Debug("will attempt to cache on disk, cache key ", keystr)
		err = diskCache.Put(keystr, encBuf.Bytes())
		if err != nil {
			log.Debug("will not cache on disk: ", err)
		}

	}

	w.Header().Set("ContentType", *result.ContentType)

	if result.ETag != nil {
		w.Header().Set("ETag", *result.ETag)
	}

	w.Write(media.Body)
}

func main() {
	kingpin.Version(version).Author("Charle Demers")
	kingpin.CommandLine.Help = "An application that exposes, in HTTP, the content of an S3 bucket in Read-Only mode, using the S3 API. It has multi-level cachign capabilities to mitigate S3 latency without sacrificing too much resources on the s3server side."
	kingpin.Parse()

	if *debugMode {
		log.SetLevel(log.DebugLevel)
	}

	diskCacheSizeBytes, err := units.FromHumanSize(*maxDiskCacheItemSize)
	if err != nil {
		log.Fatal(err)
	}

	diskCache, err = stash.New(*diskCacheFolder, int64(diskCacheSizeBytes), *maxDiskCacheItemNumber)
	if err != nil {
		log.Fatal(err)
	}

	ramCacheSizeBytes, err := units.FromHumanSize(*maxRamCacheSize)
	if err != nil {
		log.Fatal(err)
	}

	ramCache = freecache.NewCache(int(ramCacheSizeBytes))
	debug.SetGCPercent(20)

	log.Infof("Starting s3server v%s with a %s RAM cache, %s MDCOS, %d MDCON, DCPATH %s\n",
		version,
		units.HumanSize(float64(ramCacheSizeBytes)),
		units.HumanSize(float64(diskCacheSizeBytes)),
		*maxDiskCacheItemNumber,
		*diskCacheFolder,
	)

	// router := mux.NewRouter()
	// router.HandleFunc("/", handler)

	// loggedRouter := handlers.LoggingHandler(os.Stdout, r)
	// http.ListenAndServe(":80", loggedRouter)

	// http.HandleFunc("/", handlers.LoggingHandler(os.Stdout, handler))
	// http.ListenAndServe(":80", nil)

	// loggerWithConfigMiddleware := logger.New(logger.Options{
	// 	Prefix:               "MySampleWebApp",
	// 	RemoteAddressHeaders: []string{"X-Forwarded-For"},
	// })

	// httpLogger := logger.New()
	// app := httpLogger.Handler(http.HandlerFunc(handler))
	// app := httpLogger.Handler(router)

	// http.HandleFunc("/", handler)

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Heartbeat(*heartbeatRoute))
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Mount("/", http.HandlerFunc(handler))

	// httpLogger := middleware.Logger(http.HandlerFunc(handler))

	// router.Use(middleware.Timeout(30 * time.Second))
	// router.Get("/", handler)

	log.Debugf("starting the http server")
	err = http.ListenAndServe(":80", router)
	if err != nil {
		log.Error(err)
	}
	log.Debugf("shutting down the http server")
}
