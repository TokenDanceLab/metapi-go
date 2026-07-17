package proxyhandler

import (
	"net/http/httptest"
	"testing"
)

func TestHandleFilesList_Unauthorized(t *testing.T) {
	SetUpstreamConfig(nil)
	req := httptest.NewRequest("GET", "/v1/files", nil)
	rec := httptest.NewRecorder()
	HandleFilesList(rec, req)
	if rec.Code != 401 {
		t.Fatalf("expected 401 without auth, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFilesUpload_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/files", nil)
	rec := httptest.NewRecorder()
	HandleFilesUpload(rec, req)
	if rec.Code != 401 {
		t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFilesDownload_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/files/test/content", nil)
	rec := httptest.NewRecorder()
	HandleFilesDownload(rec, req)
	if rec.Code != 401 {
		t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFilesInfo_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/files/test", nil)
	rec := httptest.NewRecorder()
	HandleFilesInfo(rec, req)
	if rec.Code != 401 {
		t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFilesDelete_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/v1/files/test", nil)
	rec := httptest.NewRecorder()
	HandleFilesDelete(rec, req)
	if rec.Code != 401 {
		t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFiles_NotHard501(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/files", nil)
	rec := httptest.NewRecorder()
	HandleFilesUpload(rec, req)
	if rec.Code == 501 {
		t.Fatalf("unexpected hard 501 stub: %s", rec.Body.String())
	}
}

func TestFilesPathID(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/files/abc123/content", nil)
	if got := filesPathID(req, "fileId"); got != "abc123" {
		t.Fatalf("got %q", got)
	}
}
