package client_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dpopsuev/battery/translate"
	"github.com/dpopsuev/scribe/client"
)

func TestPost_SendsNDJSON(t *testing.T) {
	var body string
	var method string
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		contentType = r.Header.Get("Content-Type")
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.WriteHeader(http.StatusMultiStatus)
	}))
	defer srv.Close()

	records := []translate.Record{
		{ID: "test/auth", Kind: "knowledge.source", Title: "Auth", Labels: []string{"source:test"}},
		{ID: "test/db", Kind: "knowledge.source", Title: "DB", Labels: []string{"source:test"}},
	}
	edges := []translate.Edge{
		{From: "test/auth", Relation: "depends_on", To: "test/db"},
	}

	err := client.Post(context.Background(), records, edges, "test-source", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost {
		t.Errorf("method = %s; want POST", method)
	}
	if contentType != "application/x-ndjson" {
		t.Errorf("content-type = %s; want application/x-ndjson", contentType)
	}
	if !strings.Contains(body, `"type":"node"`) {
		t.Error("body missing node records")
	}
	if !strings.Contains(body, `"type":"edge"`) {
		t.Error("body missing edge records")
	}
	if !strings.Contains(body, `"type":"meta"`) {
		t.Error("body missing meta record")
	}
	if !strings.Contains(body, "test/auth") {
		t.Error("body missing record ID")
	}
	if !strings.Contains(body, "depends_on") {
		t.Error("body missing edge relation")
	}
}

func TestPost_SourceQueryParam(t *testing.T) {
	var url string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		url = r.URL.String()
		w.WriteHeader(http.StatusMultiStatus)
	}))
	defer srv.Close()

	_ = client.Post(context.Background(), nil, nil, "locus", srv.URL)
	if !strings.Contains(url, "source=locus") {
		t.Errorf("URL = %s; want source=locus param", url)
	}
}

func TestPost_ReturnsErrorOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	err := client.Post(context.Background(), nil, nil, "test", srv.URL)
	if err == nil {
		t.Error("expected error on 400")
	}
}

func TestPost_EmptyRecords(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.WriteHeader(http.StatusMultiStatus)
	}))
	defer srv.Close()

	err := client.Post(context.Background(), nil, nil, "empty", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"total_nodes":0`) {
		t.Error("meta should show 0 nodes")
	}
}
