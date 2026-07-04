package proxy

import (
	"net/http/httptest"
	"testing"
)

func TestHandleFilesList_Empty(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/files", nil)
	rec := httptest.NewRecorder()
	HandleFilesList(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := unmarshalResponse(t, rec)
	if m["object"] != "list" {
		t.Errorf("object = %v", m["object"])
	}
	data, _ := m["data"].([]any)
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d items", len(data))
	}
}

func TestHandleFilesUpload_Returns501(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/files", nil)
	rec := httptest.NewRecorder()
	HandleFilesUpload(rec, req)

	if rec.Code != 501 {
		t.Errorf("expected 501, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFilesDownload_Returns501(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/files/test/content", nil)
	rec := httptest.NewRecorder()
	HandleFilesDownload(rec, req)

	if rec.Code != 501 {
		t.Errorf("expected 501, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFilesInfo_Returns501(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/files/test", nil)
	rec := httptest.NewRecorder()
	HandleFilesInfo(rec, req)

	if rec.Code != 501 {
		t.Errorf("expected 501, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFilesDelete_Returns501(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/v1/files/test", nil)
	rec := httptest.NewRecorder()
	HandleFilesDelete(rec, req)

	if rec.Code != 501 {
		t.Errorf("expected 501, got %d: %s", rec.Code, rec.Body.String())
	}
}
