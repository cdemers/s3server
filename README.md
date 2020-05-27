
# s3server

[NAME](#NAME)  
[SYNOPSIS](#SYNOPSIS)  
[DESCRIPTION](#DESCRIPTION)  
[OPTIONS](#OPTIONS)  

----------

## NAME

s3server

## SYNOPSIS

**s3server --s3-bucket=S3_BUCKET [\<flags\>]**

## DESCRIPTION

An application that exposes, in HTTP, the content of an S3 bucket in Read-Only mode, using the S3 API. It has
multi-level cachign capabilities to mitigate S3 latency without sacrificing too much resources on the s3server side.

Authentication to AWS S3 is done using the AWS convention (the `~/.aws` folder, the environment variables, etc).

## OPTIONS

**--help**

Show context-sensitive help (also try --help-long and --help-man).

**--debug**

Debug mode.

**--heartbeat-route="/health"**

The HTTP route of the heartbeat check, include the slashes when necessary (env: HEARTBEAT_ROUTE).

**--s3-bucket=S3_BUCKET**

The AWS S3 Bucket name (env: S3_BUCKET).

**--ram-cache-size="300 MB"**

The RAM cache size in human format, ex "300 MB" (env: RAM_CACHE_SIZE).

**--disk-cache-item-size="50 MB"**

The disk cache maximum object size, in human format, ex "50 MB" (env: DISK_CACHE_OBJECT_SIZE).

**--disk-cache-item-number=20**

The maximum number of disk cache objects, ex "20" (env: DISK_CACHE_OBJECT_NUMBER).

**--disk-cache-path="/tmp"**

The path of the disk cache folder, enough space must be available for the configured disk cache (env: DISK_CACHE_PATH).

**--version**

Show application version.

## Health Check

To be used for liveness and readyness probes, or as any heartbeat check.

Heath check route is available at `/health` by default.  This check covers some
HTTP server health specifications, and querying this will return status code
200, and a dot "." as the response body.


## References

**Go**

* https://stackoverflow.com/questions/20554175/how-check-if-a-property-was-set-in-a-struct
