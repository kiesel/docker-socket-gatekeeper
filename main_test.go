package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStripVersionPrefix(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "with v1.40 prefix",
			input:  "/v1.40/containers/json",
			expect: "/containers/json",
		},
		{
			name:   "with v1.41 prefix",
			input:  "/v1.41/images/list",
			expect: "/images/list",
		},
		{
			name:   "with v1 prefix",
			input:  "/v1/volumes/list",
			expect: "/volumes/list",
		},
		{
			name:   "without version prefix",
			input:  "/_ping",
			expect: "/_ping",
		},
		{
			name:   "without version prefix slash",
			input:  "/info",
			expect: "/info",
		},
		{
			name:   "no leading slash",
			input:  "containers",
			expect: "containers",
		},
		{
			name:   "complex path with query",
			input:  "/v1.40/containers/json?all=true",
			expect: "/containers/json?all=true",
		},
		{
			name:   "empty string",
			input:  "",
			expect: "",
		},
		{
			name:   "just version, no path",
			input:  "/v1.40",
			expect: "/v1.40",
		},
		{
			name:   "invalid version format",
			input:  "/vx.y/containers",
			expect: "/vx.y/containers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripVersionPrefix(tt.input)
			if result != tt.expect {
				t.Errorf("stripVersionPrefix(%q) = %q, want %q", tt.input, result, tt.expect)
			}
		})
	}
}

func TestIsVersionSegment(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{
			name:   "valid v1.40",
			input:  "v1.40",
			expect: true,
		},
		{
			name:   "valid v1.41",
			input:  "v1.41",
			expect: true,
		},
		{
			name:   "valid v1",
			input:  "v1",
			expect: true,
		},
		{
			name:   "no v prefix",
			input:  "1.40",
			expect: false,
		},
		{
			name:   "too many parts",
			input:  "v1.40.0",
			expect: false,
		},
		{
			name:   "non-numeric",
			input:  "vx.y",
			expect: false,
		},
		{
			name:   "empty",
			input:  "",
			expect: false,
		},
		{
			name:   "just v",
			input:  "v",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVersionSegment(tt.input)
			if result != tt.expect {
				t.Errorf("isVersionSegment(%q) = %v, want %v", tt.input, result, tt.expect)
			}
		})
	}
}

func TestIsPathAllowed(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		allowed []string
		expect  bool
	}{
		{
			name:    "exact match",
			path:    "/containers",
			allowed: []string{"/containers"},
			expect:  true,
		},
		{
			name:    "prefix match",
			path:    "/containers/json",
			allowed: []string{"/containers"},
			expect:  true,
		},
		{
			name:    "prefix match with query",
			path:    "/containers/json?all=true",
			allowed: []string{"/containers"},
			expect:  true,
		},
		{
			name:    "no match",
			path:    "/images/list",
			allowed: []string{"/containers"},
			expect:  false,
		},
		{
			name:    "multiple allowed, first matches",
			path:    "/containers/ps",
			allowed: []string{"/containers", "/images"},
			expect:  true,
		},
		{
			name:    "multiple allowed, second matches",
			path:    "/images/list",
			allowed: []string{"/containers", "/images"},
			expect:  true,
		},
		{
			name:    "multiple allowed, none match",
			path:    "/volumes/ls",
			allowed: []string{"/containers", "/images"},
			expect:  false,
		},
		{
			name:    "system endpoint _ping",
			path:    "/_ping",
			allowed: []string{"/_ping", "/version", "/info"},
			expect:  true,
		},
		{
			name:    "system endpoint version",
			path:    "/version",
			allowed: []string{"/_ping", "/version", "/info"},
			expect:  true,
		},
		{
			name:    "empty allowed list",
			path:    "/containers",
			allowed: []string{},
			expect:  false,
		},
		{
			name:    "partial prefix mismatch",
			path:    "/container",
			allowed: []string{"/containers"},
			expect:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathAllowed(tt.path, tt.allowed)
			if result != tt.expect {
				t.Errorf("isPathAllowed(%q, %v) = %v, want %v", tt.path, tt.allowed, result, tt.expect)
			}
		})
	}
}

// Test the combination of stripVersionPrefix and isPathAllowed
func TestStripAndAllow(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		allowed []string
		expect  bool
	}{
		{
			name:    "strip v1.40 then match",
			path:    "/v1.40/containers/json",
			allowed: []string{"/containers"},
			expect:  true,
		},
		{
			name:    "strip v1.41 then match images",
			path:    "/v1.41/images/list",
			allowed: []string{"/images"},
			expect:  true,
		},
		{
			name:    "strip then deny",
			path:    "/v1.40/volumes/list",
			allowed: []string{"/containers", "/images"},
			expect:  false,
		},
		{
			name:    "no version, match",
			path:    "/_ping",
			allowed: []string{"/_ping"},
			expect:  true,
		},
		{
			name:    "no version, deny",
			path:    "/_ping",
			allowed: []string{"/containers"},
			expect:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := stripVersionPrefix(tt.path)
			result := isPathAllowed(normalized, tt.allowed)
			if result != tt.expect {
				t.Errorf("stripVersionPrefix+isPathAllowed(%q) with allowed %v = %v, want %v",
					tt.path, tt.allowed, result, tt.expect)
			}
		})
	}
}

func TestParseListen(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expProto string
		expAddr  string
		expError bool
	}{
		{
			name:     "unix socket",
			input:    "unix:/var/run/docker.sock",
			expProto: "unix",
			expAddr:  "/var/run/docker.sock",
		},
		{
			name:     "tcp with address",
			input:    "tcp:127.0.0.1:2375",
			expProto: "tcp",
			expAddr:  "127.0.0.1:2375",
		},
		{
			name:     "fallback tcp (colon present)",
			input:    "0.0.0.0:2375",
			expProto: "tcp",
			expAddr:  "0.0.0.0:2375",
		},
		{
			name:     "invalid no prefix",
			input:    "localhost",
			expError: true,
		},
		{
			name:     "invalid empty",
			input:    "",
			expError: true,
		},
		{
			name:     "unix with relative path",
			input:    "unix:./docker.sock",
			expProto: "unix",
			expAddr:  "./docker.sock",
		},
		{
			name:     "tcp localhost",
			input:    "tcp:localhost:9999",
			expProto: "tcp",
			expAddr:  "localhost:9999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proto, addr, err := parseListen(tt.input)
			if tt.expError {
				if err == nil {
					t.Errorf("parseListen(%q) expected error, got none", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseListen(%q) unexpected error: %v", tt.input, err)
				return
			}
			if proto != tt.expProto {
				t.Errorf("parseListen(%q) proto = %q, want %q", tt.input, proto, tt.expProto)
			}
			if addr != tt.expAddr {
				t.Errorf("parseListen(%q) addr = %q, want %q", tt.input, addr, tt.expAddr)
			}
		})
	}
}

func TestIsVersionSegmentEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{
			name:   "v with dot but no minor",
			input:  "v.",
			expect: false,
		},
		{
			name:   "v with trailing dot",
			input:  "v1.",
			expect: false,
		},
		{
			name:   "double digit version",
			input:  "v12.34",
			expect: true,
		},
		{
			name:   "version with leading zero",
			input:  "v01.40",
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVersionSegment(tt.input)
			if result != tt.expect {
				t.Errorf("isVersionSegment(%q) = %v, want %v", tt.input, result, tt.expect)
			}
		})
	}
}

func TestStripVersionPrefixEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "version with extra slashes",
			input:  "/v1.40//containers",
			expect: "//containers",
		},
		{
			name:   "version with trailing slash only",
			input:  "/v1.40/",
			expect: "/",
		},
		{
			name:   "deeply nested path",
			input:  "/v1.40/containers/abc123/logs?follow=true",
			expect: "/containers/abc123/logs?follow=true",
		},
		{
			name:   "path with fragment",
			input:  "/v1.40/info#top",
			expect: "/info#top",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripVersionPrefix(tt.input)
			if result != tt.expect {
				t.Errorf("stripVersionPrefix(%q) = %q, want %q", tt.input, result, tt.expect)
			}
		})
	}
}

func TestIsPathAllowedEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		allowed []string
		expect  bool
	}{
		{
			name:    "path with uppercase (case sensitive)",
			path:    "/Containers/json",
			allowed: []string{"/containers"},
			expect:  false,
		},
		{
			name:    "allowed with trailing slash",
			path:    "/containers/json",
			allowed: []string{"/containers/"},
			expect:  true,
		},
		{
			name:    "single slash match",
			path:    "/",
			allowed: []string{"/"},
			expect:  true,
		},
		{
			name:    "multiple leading slashes",
			path:    "//containers",
			allowed: []string{"/containers"},
			expect:  false,
		},
		{
			name:    "allowed with wildcards (treated literally)",
			path:    "/containers/abc",
			allowed: []string{"/containers/*"},
			expect:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathAllowed(tt.path, tt.allowed)
			if result != tt.expect {
				t.Errorf("isPathAllowed(%q, %v) = %v, want %v", tt.path, tt.allowed, result, tt.expect)
			}
		})
	}
}

func TestIsMethodAllowed(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		allowed []string
		expect  bool
	}{
		{
			name:    "GET allowed",
			method:  "GET",
			allowed: []string{"GET", "POST"},
			expect:  true,
		},
		{
			name:    "POST allowed",
			method:  "POST",
			allowed: []string{"GET", "POST"},
			expect:  true,
		},
		{
			name:    "DELETE not allowed",
			method:  "DELETE",
			allowed: []string{"GET", "POST"},
			expect:  false,
		},
		{
			name:    "empty allowed list",
			method:  "GET",
			allowed: []string{},
			expect:  false,
		},
		{
			name:    "case sensitive",
			method:  "get",
			allowed: []string{"GET"},
			expect:  false,
		},
		{
			name:    "single method",
			method:  "HEAD",
			allowed: []string{"HEAD"},
			expect:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMethodAllowed(tt.method, tt.allowed)
			if result != tt.expect {
				t.Errorf("isMethodAllowed(%q, %v) = %v, want %v", tt.method, tt.allowed, result, tt.expect)
			}
		})
	}
}

func TestHTTPHandler(t *testing.T) {
	// Mock allowed paths
	allowed := []string{"/containers", "/images", "/_ping"}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		normalized := stripVersionPrefix(r.URL.Path)
		if !isPathAllowed(normalized, allowed) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	tests := []struct {
		name         string
		path         string
		expectedCode int
	}{
		{
			name:         "allowed path /containers",
			path:         "/containers",
			expectedCode: http.StatusOK,
		},
		{
			name:         "allowed path with version",
			path:         "/v1.40/containers/json",
			expectedCode: http.StatusOK,
		},
		{
			name:         "allowed system path",
			path:         "/_ping",
			expectedCode: http.StatusOK,
		},
		{
			name:         "denied path /volumes",
			path:         "/volumes",
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "denied path with version",
			path:         "/v1.40/volumes/list",
			expectedCode: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("handler for %q returned code %d, want %d", tt.path, w.Code, tt.expectedCode)
			}
		})
	}
}
