// Backblaze B2 (S3-compatible) WORM archive client.
// Uploads with Object Lock COMPLIANCE retention so recordings cannot be
// deleted until retention expires — satisfies TRAI/DPDP archival rules.
package recording

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// httpClientWithDNS returns an *http.Client whose Transport.DialContext
// performs explicit name resolution against CUSTOM_DNS (comma-separated)
// before dialing. This bypasses the OS resolver entirely — needed when
// ISP DNS (e.g. Jio) blocks lookups for providers like Backblaze.
//
// When CUSTOM_DNS is empty, returns http.DefaultClient unchanged.
func httpClientWithDNS() *http.Client {
	dnsList := strings.TrimSpace(os.Getenv("CUSTOM_DNS"))
	if dnsList == "" {
		return http.DefaultClient
	}
	servers := make([]string, 0, 4)
	for _, s := range strings.Split(dnsList, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, ":") {
			s += ":53"
		}
		servers = append(servers, s)
	}
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			var lastErr error
			for _, s := range servers {
				c, err := d.DialContext(ctx, "udp", s)
				if err == nil {
					return c, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
	net.DefaultResolver = resolver

	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return dialer.DialContext(ctx, network, addr)
			}
			ips, err := resolver.LookupIPAddr(ctx, host)
			if err != nil || len(ips) == 0 {
				return nil, fmt.Errorf("custom-dns lookup %s: %v", host, err)
			}
			var lastErr error
			for _, ip := range ips {
				c, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if derr == nil {
					return c, nil
				}
				lastErr = derr
			}
			return nil, lastErr
		},
		MaxIdleConns:    16,
		IdleConnTimeout: 90 * time.Second,
	}
	return &http.Client{Timeout: 120 * time.Second, Transport: transport}
}

type b2Client struct {
	s3            *s3.Client
	bucket        string
	endpoint      string
	region        string
	retentionDays int
}

// B2 application key IDs encode the realm: the first 3 chars give the region.
// 005 → us-west-005, 002 → us-west-002, 001 → eu-central-003, etc.
func b2RegionFromKey(keyID string) string {
	if len(keyID) < 3 {
		return "us-west-005"
	}
	switch keyID[:3] {
	case "000", "001":
		return "us-west-002"
	case "002":
		return "us-west-002"
	case "003":
		return "eu-central-003"
	case "004":
		return "us-west-004"
	case "005":
		return "us-west-005"
	case "006":
		return "us-west-006"
	default:
		return "us-west-005"
	}
}

// NewB2 constructs a B2 client via the S3-compatible endpoint.
// retentionDays defaults to 365 if 0.
func NewB2(key, secret, bucket string, retentionDays int) (*b2Client, error) {
	if key == "" || secret == "" || bucket == "" {
		return nil, fmt.Errorf("b2: missing creds (key/secret/bucket)")
	}
	if retentionDays <= 0 {
		retentionDays = 365
	}
	region := b2RegionFromKey(key)
	endpoint := fmt.Sprintf("https://s3.%s.backblazeb2.com", region)
	client := s3.New(s3.Options{
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider(key, secret, ""),
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: false,
		HTTPClient:   httpClientWithDNS(),
	})
	return &b2Client{
		s3:            client,
		bucket:        bucket,
		endpoint:      endpoint,
		region:        region,
		retentionDays: retentionDays,
	}, nil
}

// Archive uploads `data` to B2 with COMPLIANCE retention.
// Returns the storage key "b2://<bucket>/<key>".
// If the bucket does not have Object Lock enabled, falls back to plain
// upload and returns an error containing "object-lock-not-enabled" so the
// caller can surface this to ops without aborting the recording pipeline.
func (c *b2Client) Archive(ctx context.Context, tenantID, key string, data []byte) (string, error) {
	until := time.Now().UTC().Add(time.Duration(c.retentionDays) * 24 * time.Hour)
	in := &s3.PutObjectInput{
		Bucket:                    aws.String(c.bucket),
		Key:                       aws.String(key),
		Body:                      bytes.NewReader(data),
		ContentType:               aws.String(detectContentType(key)),
		ObjectLockMode:            types.ObjectLockModeCompliance,
		ObjectLockRetainUntilDate: aws.Time(until),
		Metadata:                  map[string]string{"tenant-id": tenantID},
	}
	if _, err := c.s3.PutObject(ctx, in); err != nil {
		if isObjectLockNotEnabled(err) {
			// Retry without lock so recording is at least archived.
			in.ObjectLockMode = ""
			in.ObjectLockRetainUntilDate = nil
			if _, err2 := c.s3.PutObject(ctx, in); err2 != nil {
				return "", fmt.Errorf("b2 archive (fallback): %w", err2)
			}
			return fmt.Sprintf("b2://%s/%s", c.bucket, key),
				fmt.Errorf("b2 archive: object-lock-not-enabled on bucket %s — uploaded without retention", c.bucket)
		}
		return "", fmt.Errorf("b2 archive: %w", err)
	}
	return fmt.Sprintf("b2://%s/%s", c.bucket, key), nil
}

// TryDelete attempts to delete a B2 object. WORM-protected objects MUST
// return an error — that's how we prove COMPLIANCE retention works.
func (c *b2Client) TryDelete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	return err
}

// Head returns true if the object exists in B2.
func (c *b2Client) Head(ctx context.Context, key string) (bool, error) {
	_, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *b2Client) Bucket() string        { return c.bucket }
func (c *b2Client) RetentionDays() int    { return c.retentionDays }

func isObjectLockNotEnabled(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "object lock") ||
		strings.Contains(msg, "objectlockconfigurationnotfoundError") ||
		strings.Contains(msg, "invalidrequest") &&
			strings.Contains(msg, "lock")
}
