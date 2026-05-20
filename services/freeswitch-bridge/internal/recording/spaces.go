// DO Spaces (S3-compatible) implementation of SpacesClient.
package recording

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type spacesClient struct {
	s3       *s3.Client
	presign  *s3.PresignClient
	bucket   string
	region   string
	endpoint string
	sse      types.ServerSideEncryption
}

// NewSpaces builds a DO Spaces client. region is e.g. "sgp1", "blr1", "nyc3".
// SSE: DO Spaces supports SSE-S3 (AES256). Pass empty to disable.
func NewSpaces(key, secret, region, bucket string) (*spacesClient, error) {
	if key == "" || secret == "" || region == "" || bucket == "" {
		return nil, fmt.Errorf("spaces: missing creds (key/secret/region/bucket)")
	}
	endpoint := fmt.Sprintf("https://%s.digitaloceanspaces.com", region)
	client := s3.New(s3.Options{
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider(key, secret, ""),
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: false,
	})
	return &spacesClient{
		s3:       client,
		presign:  s3.NewPresignClient(client),
		bucket:   bucket,
		region:   region,
		endpoint: endpoint,
		sse:      types.ServerSideEncryptionAes256,
	}, nil
}

// Put uploads to DO Spaces. Returns a stable storage key of the form
// "do-spaces://<bucket>/<key>". `encrypted=true` requests SSE-S3.
func (c *spacesClient) Put(ctx context.Context, tenantID, key string, data []byte, encrypted bool) (string, error) {
	in := &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(detectContentType(key)),
		Metadata:    map[string]string{"tenant-id": tenantID},
	}
	if encrypted {
		in.ServerSideEncryption = c.sse
	}
	if _, err := c.s3.PutObject(ctx, in); err != nil {
		// DO Spaces in some regions rejects SSE; retry once without it.
		if encrypted && isSSEError(err) {
			in.ServerSideEncryption = ""
			if _, err2 := c.s3.PutObject(ctx, in); err2 != nil {
				return "", fmt.Errorf("spaces put (no-sse retry): %w", err2)
			}
		} else {
			return "", fmt.Errorf("spaces put: %w", err)
		}
	}
	return fmt.Sprintf("do-spaces://%s/%s", c.bucket, key), nil
}

// PresignDownload returns a short-lived GET URL for dashboard playback.
func (c *spacesClient) PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := c.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, func(o *s3.PresignOptions) { o.Expires = ttl })
	if err != nil {
		return "", fmt.Errorf("spaces presign: %w", err)
	}
	return req.URL, nil
}

// Delete removes an object (used by retention cron — hot tier only).
func (c *spacesClient) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("spaces delete: %w", err)
	}
	return nil
}

// List returns object keys older than `olderThan`. Used by retention cron.
func (c *spacesClient) ListOlderThan(ctx context.Context, prefix string, olderThan time.Time) ([]string, error) {
	var out []string
	var token *string
	for {
		page, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("spaces list: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.LastModified != nil && obj.LastModified.Before(olderThan) && obj.Key != nil {
				out = append(out, *obj.Key)
			}
		}
		if page.IsTruncated == nil || !*page.IsTruncated {
			break
		}
		token = page.NextContinuationToken
	}
	return out, nil
}

func (c *spacesClient) Bucket() string { return c.bucket }

func detectContentType(key string) string {
	switch {
	case strings.HasSuffix(key, ".wav"):
		return "audio/wav"
	case strings.HasSuffix(key, ".mp3"):
		return "audio/mpeg"
	case strings.HasSuffix(key, ".opus"), strings.HasSuffix(key, ".ogg"):
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}

func isSSEError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "encryption") || strings.Contains(msg, "sse") ||
		strings.Contains(msg, "notimplemented")
}
