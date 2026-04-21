package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// UploadTimeout caps a single presigned PUT. Must comfortably exceed the time
// to stream a 5 GB file over a slow link but stay under the 15-minute presigned
// URL expiry.
const UploadTimeout = 10 * time.Minute

// PutPresigned streams body to a presigned URL via HTTP PUT.
//
// Use this for AWS S3 presigned URLs where only "host" is signed. The caller's
// authenticated client must NOT be used — presigned URLs are meant for public
// access with URL-signed credentials, and sending an unrelated x-api-key or
// Bearer token to a third-party host is a credential-leak risk.
//
// contentLength must match the size of body; S3 rejects requests with a
// missing or incorrect Content-Length. No Content-Type is set because the
// presigned URLs issued by JumpCloud sign only the host header.
func PutPresigned(ctx context.Context, url string, body io.Reader, contentLength int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, body)
	if err != nil {
		return fmt.Errorf("creating upload request: %w", err)
	}
	req.ContentLength = contentLength

	client := &http.Client{
		Timeout:   UploadTimeout,
		Transport: baseTransport(),
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("uploading to presigned URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("presigned upload failed: HTTP %d: %s", resp.StatusCode, string(snippet))
	}
	return nil
}
