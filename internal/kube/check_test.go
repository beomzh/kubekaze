package kube

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
)

func TestCheckUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Unauthorized","code":401}`))
	}))
	defer srv.Close()
	err := Check(context.Background(), &rest.Config{Host: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "Unauthorized") || !strings.Contains(err.Error(), "log in again") {
		t.Errorf("err = %v", err)
	}
}

func TestCheckOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"major":"1","minor":"30"}`))
	}))
	defer srv.Close()
	if err := Check(context.Background(), &rest.Config{Host: srv.URL}); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestCheckForbiddenIsReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`))
	}))
	defer srv.Close()
	if err := Check(context.Background(), &rest.Config{Host: srv.URL}); err != nil {
		t.Errorf("forbidden means reachable and authenticated, got %v", err)
	}
}

func TestCheckUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	host := srv.URL
	srv.Close() // nothing listens any more
	err := Check(context.Background(), &rest.Config{Host: host})
	if err == nil || !strings.Contains(err.Error(), "API server") {
		t.Errorf("err = %v", err)
	}
}
