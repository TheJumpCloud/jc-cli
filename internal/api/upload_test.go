package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPutPresigned_OK(t *testing.T) {
	var gotMethod, gotAuth, gotAPIKey string
	var gotBody []byte
	var gotLen int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-Api-Key")
		gotLen = r.ContentLength
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body := []byte("package bytes")
	err := PutPresigned(context.Background(), srv.URL+"/upload", bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("PutPresigned err: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if !bytes.Equal(gotBody, body) {
		t.Errorf("body = %q, want %q", gotBody, body)
	}
	if gotLen != int64(len(body)) {
		t.Errorf("Content-Length = %d, want %d", gotLen, len(body))
	}
	// Critical: must not leak API credentials to the presigned host.
	if gotAuth != "" {
		t.Errorf("Authorization header should be empty, got %q", gotAuth)
	}
	if gotAPIKey != "" {
		t.Errorf("X-Api-Key header should be empty, got %q", gotAPIKey)
	}
}

func TestPutPresigned_ErrorSurfacesResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "<Error><Code>AccessDenied</Code></Error>")
	}))
	defer srv.Close()

	err := PutPresigned(context.Background(), srv.URL, strings.NewReader("x"), 1)
	if err == nil {
		t.Fatal("expected error for 403 response, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention 403, got: %v", err)
	}
	if !strings.Contains(err.Error(), "AccessDenied") {
		t.Errorf("error should include response snippet, got: %v", err)
	}
}
