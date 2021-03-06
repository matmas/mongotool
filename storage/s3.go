package storage

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/smartystreets/go-aws-auth"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var signMu sync.Mutex

// requestBuilder is something that can sign and return a http.Request for S3.
type requestBuilder func(method, bucket, path string, body io.Reader) (req *http.Request, err error)

// s3FileWriter takes care of buffering all written data for one S3 object until ready to be sent.
type s3FileWriter struct {
	bytes.Buffer
	path    string
	bucket  string
	builder requestBuilder
	closed  bool
}

func news3FileWriter(bucket, path string, builder requestBuilder) *s3FileWriter {
	sf := s3FileWriter{
		bucket:  bucket,
		path:    path,
		builder: builder,
	}
	return &sf
}

// Close will send the buffered data to S3 using the requestBuilder.
func (sf *s3FileWriter) Close() error {
	if sf.closed {
		return nil
	}
	sf.closed = true

	req, err := sf.builder("PUT", sf.bucket, sf.path, bytes.NewReader(sf.Bytes()))
	if err != nil {
		return err
	}
	client := http.DefaultClient

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if code := resp.StatusCode; code != 200 {
		msg, _ := ioutil.ReadAll(resp.Body)
		return errors.New(
			fmt.Sprintf("Expected 200 OK, got: (%d)\n%s", code, string(msg)),
		)
	}

	return nil
}

// S3 implements the SaveFetcher for Amazon S3.
type S3 struct {
	// The full path to the bucket host.
	// Example: https://mongotool.s3.amazonaws.com
	Bucket string
	client *http.Client
}

func NewS3(bucket string) *S3 {
	return &S3{
		bucket,
		&http.Client{
			// For some reason S3 will mess up subsequent GET's if keep alive.
			Transport: &http.Transport{DisableKeepAlives: true},
		},
	}
}

// checkAwsKeys will look for they environment variables implicitly used by go-aws-auth
func (s S3) checkAwsKeys() error {
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" {
		return errors.New("Missing AWS_ACCESS_KEY_ID environment variable")
	}
	if os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
		return errors.New("Missing AWS_SECRET_ACCESS_KEY environment variable")
	}
	return nil
}

func (s S3) Save(path string) (io.WriteCloser, error) {
	if err := s.checkAwsKeys(); err != nil {
		return nil, err
	}
	return news3FileWriter(s.Bucket, path, S3ObjectReq), nil
}

func (s S3) Walk(p string, walkfn WalkFunc) error {
	if err := s.checkAwsKeys(); err != nil {
		return err
	}
	p = strings.TrimLeft(p, "/")
	if string(p[0]) != "/" {
		p += "/"
	}
	req, err := http.NewRequest("GET", s.Bucket, nil)
	if err != nil {
		return err
	}
	params := req.URL.Query()
	params.Set("prefix", p)
	req.URL.RawQuery = params.Encode()

	signMu.Lock()
	awsauth.Sign4(req)
	signMu.Unlock()

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	respBody, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	if code := resp.StatusCode; code != http.StatusOK {
		return errors.New(fmt.Sprintf("Unexpected status code: %d\n%s", code, string(respBody)))
	}

	// FIXME: Limited to returning 1000 objects, the rest has to be iterated in follow up requests
	bucketlist := struct {
		Contents []struct {
			Key          string
			LastModified time.Time
			Size         int64
		}
	}{}

	err = xml.Unmarshal(respBody, &bucketlist)
	if err != nil {
		return err
	}
	for _, entry := range bucketlist.Contents {
		walkfn(entry.Key, nil)
	}
	return nil
}

func (s S3) Fetch(path string) (io.ReadCloser, error) {
	if err := s.checkAwsKeys(); err != nil {
		return nil, err
	}
	req, err := S3ObjectReq("GET", s.Bucket, path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, err
	}
	if code := resp.StatusCode; code != http.StatusOK {
		// Don't output body here as it might be a huge file and we can return the body directly
		return nil, errors.New(fmt.Sprintf("Unexpected status code: %d\n%s", code))
	}

	return resp.Body, nil
}

func fullPath(bucket, path string) string {
	if len(path) > 0 {
		if string(path[0]) != "/" && string(bucket[len(bucket)-1]) != "/" {
			path = "/" + path
		}
	}
	return bucket + path
}

func S3ObjectReq(method, bucket, path string, body io.Reader) (req *http.Request, err error) {
	if req, err = http.NewRequest(method, fullPath(bucket, path), body); err != nil {
		return
	}

	signMu.Lock()
	awsauth.Sign4(req)
	signMu.Unlock()
	return
}
