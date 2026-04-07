package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gerolf-vent/mikrolb/internal/routeros/mock"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// NewMockClient creates a Client backed by a mockRouterOS test server.
// The server is automatically closed when the test completes.
func NewMockClient(t *testing.T, ros *mock.RouterOS) *Client {
	t.Helper()
	server := httptest.NewServer(ros.Handler())
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse test server URL: %v", err)
	}

	zapLog, err := zap.NewDevelopment()
	if err != nil {
		panic(fmt.Sprintf("who watches the watchmen (%v)?", err))
	}
	log := zapr.NewLogger(zapLog)

	return NewClient(u, nil, log)
}

func NewCustomHandlerClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse test server URL: %v", err)
	}

	zapLog, err := zap.NewDevelopment()
	if err != nil {
		panic(fmt.Sprintf("who watches the watchmen (%v)?", err))
	}
	logger := zapr.NewLogger(zapLog)

	return NewClient(u, nil, logger)
}

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

func TestError_Error(t *testing.T) {
	err := Error{Code: 404, Message: "not found", Detail: "resource does not exist"}
	got := err.Error()
	want := "API error: code 404, message: not found, detail: resource does not exist"
	if got != want {
		t.Errorf("Error.Error() = %q, want %q", got, want)
	}
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantPath string
	}{
		{"plain endpoint", "http://router.local", "/rest"},
		{"endpoint with trailing slash", "http://router.local/", "/rest"},
		{"endpoint with existing path", "http://router.local/api", "/api/rest"},
		{"endpoint with existing path and trailing slash", "http://router.local/api/", "/api/rest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, _ := url.Parse(tt.endpoint)
			c := NewClient(u, nil, logr.Discard())
			if c.endpoint.Path != tt.wantPath {
				t.Errorf("endpoint.Path = %q, want %q", c.endpoint.Path, tt.wantPath)
			}
		})
	}
}

func TestNewClient_DefaultHTTPClient(t *testing.T) {
	u, _ := url.Parse("http://router.local")
	c := NewClient(u, nil, logr.Discard())
	if c.httpClient == nil {
		t.Fatal("httpClient should be set by default")
	}
}

func TestSetHTTPClient(t *testing.T) {
	u, _ := url.Parse("http://router.local")
	c := NewClient(u, nil, logr.Discard())

	custom := &http.Client{}
	c.SetHTTPClient(custom)
	if c.httpClient != custom {
		t.Error("httpClient was not replaced")
	}
}

func TestSetCredentials(t *testing.T) {
	u, _ := url.Parse("http://router.local")
	c := NewClient(u, nil, logr.Discard())
	c.SetCredentials("admin", "secret")
	if c.username != "admin" {
		t.Errorf("username = %q, want %q", c.username, "admin")
	}
	if c.password != "secret" {
		t.Errorf("password = %q, want %q", c.password, "secret")
	}
}

// ---------------------------------------------------------------------------
// CRUD tests against the mock RouterOS server
// ---------------------------------------------------------------------------

func TestClient_Get(t *testing.T) {
	ros := mock.NewRouterOS()
	ros.Seed("/ip/address",
		map[string]interface{}{"address": "10.0.0.1/24", "interface": "ether1"},
	)
	client := NewMockClient(t, ros)

	result, err := client.Get("/ip/address", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !result.IsArray() {
		t.Fatal("expected array result")
	}
	arr := result.Array()
	if len(arr) != 1 {
		t.Fatalf("expected 1 element, got %d", len(arr))
	}
	if arr[0].Get("address").String() != "10.0.0.1/24" {
		t.Errorf("address = %q, want %q", arr[0].Get("address").String(), "10.0.0.1/24")
	}
}

func TestClient_Get_WithQuery(t *testing.T) {
	ros := mock.NewRouterOS()
	ros.Seed("/ip/address",
		map[string]interface{}{"address": "10.0.0.1/24", "interface": "ether1", "type": "static"},
		map[string]interface{}{"address": "192.168.1.1/24", "interface": "ether2", "type": "dynamic"},
	)
	client := NewMockClient(t, ros)

	result, err := client.Get("/ip/address", Query{"type": "static"})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	arr := result.Array()
	if len(arr) != 1 {
		t.Fatalf("expected 1 filtered result, got %d", len(arr))
	}
	if arr[0].Get("address").String() != "10.0.0.1/24" {
		t.Errorf("address = %q, want %q", arr[0].Get("address").String(), "10.0.0.1/24")
	}
}

func TestClient_Get_EmptyList(t *testing.T) {
	ros := mock.NewRouterOS()
	client := NewMockClient(t, ros)

	result, err := client.Get("/ip/address", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !result.IsArray() {
		t.Fatal("expected array result")
	}
	if len(result.Array()) != 0 {
		t.Errorf("expected empty array, got %d elements", len(result.Array()))
	}
}

func TestClient_Put(t *testing.T) {
	ros := mock.NewRouterOS()
	client := NewMockClient(t, ros)

	result, err := client.Put("/ip/address", Request{"address": "10.0.0.1/24", "interface": "ether1"})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if result.Get("\\.id").String() == "" {
		t.Error("expected .id in response")
	}
	if result.Get("address").String() != "10.0.0.1/24" {
		t.Errorf("address = %q, want %q", result.Get("address").String(), "10.0.0.1/24")
	}

	// Verify the resource was actually stored
	resources := ros.Resources("/ip/address")
	if len(resources) != 1 {
		t.Fatalf("expected 1 stored resource, got %d", len(resources))
	}
}

func TestClient_Put_MultipleResources(t *testing.T) {
	ros := mock.NewRouterOS()
	client := NewMockClient(t, ros)

	client.Put("/ip/address", Request{"address": "10.0.0.1/24", "interface": "ether1"})
	client.Put("/ip/address", Request{"address": "10.0.0.2/24", "interface": "ether2"})

	resources := ros.Resources("/ip/address")
	if len(resources) != 2 {
		t.Fatalf("expected 2 stored resources, got %d", len(resources))
	}
}

func TestClient_Patch(t *testing.T) {
	ros := mock.NewRouterOS()
	ros.Seed("/ip/address",
		map[string]interface{}{"address": "10.0.0.1/24", "interface": "ether1"},
	)
	client := NewMockClient(t, ros)

	// Get the auto-generated ID
	id := ros.Resources("/ip/address")[0][".id"].(string)

	result, err := client.Patch("/ip/address/"+id, Request{"address": "10.0.0.2/24"})
	if err != nil {
		t.Fatalf("Patch() error = %v", err)
	}
	if result.Get("address").String() != "10.0.0.2/24" {
		t.Errorf("address = %q, want %q", result.Get("address").String(), "10.0.0.2/24")
	}
	// Original field should still be present
	if result.Get("interface").String() != "ether1" {
		t.Errorf("interface = %q, want %q", result.Get("interface").String(), "ether1")
	}
}

func TestClient_Patch_RemoveField(t *testing.T) {
	ros := mock.NewRouterOS()
	ros.Seed("/ip/address",
		map[string]interface{}{"address": "10.0.0.1/24", "comment": "old comment"},
	)
	client := NewMockClient(t, ros)

	id := ros.Resources("/ip/address")[0][".id"].(string)

	// Sending nil for a field should remove it (RouterOS behaviour)
	result, err := client.Patch("/ip/address/"+id, Request{"comment": nil})
	if err != nil {
		t.Fatalf("Patch() error = %v", err)
	}
	if result.Get("comment").Exists() {
		t.Error("field 'comment' should have been removed")
	}
}

func TestClient_Delete(t *testing.T) {
	ros := mock.NewRouterOS()
	ros.Seed("/ip/address",
		map[string]interface{}{"address": "10.0.0.1/24", "interface": "ether1"},
	)
	client := NewMockClient(t, ros)

	id := ros.Resources("/ip/address")[0][".id"].(string)

	err := client.Delete("/ip/address/" + id)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	resources := ros.Resources("/ip/address")
	if len(resources) != 0 {
		t.Errorf("expected 0 resources after delete, got %d", len(resources))
	}
}

func TestClient_Delete_NotFound(t *testing.T) {
	ros := mock.NewRouterOS()
	client := NewMockClient(t, ros)

	err := client.Delete("/ip/address/*999")
	if err == nil {
		t.Fatal("expected error when deleting non-existent resource")
	}
}

func TestClient_Post(t *testing.T) {
	ros := mock.NewRouterOS()
	ros.Seed("/ip/address",
		map[string]interface{}{"address": "10.0.0.1/24", "interface": "ether1"},
		map[string]interface{}{"address": "10.0.0.2/24", "interface": "ether2"},
	)
	client := NewMockClient(t, ros)

	result, err := client.Post("/ip/address/print", nil,
		[]string{"address", "interface"},
		[]string{"interface=ether1"},
	)
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	if !result.IsArray() {
		t.Fatal("expected array result")
	}
	arr := result.Array()
	if len(arr) != 1 {
		t.Fatalf("expected 1 filtered result, got %d", len(arr))
	}
	if arr[0].Get("address").String() != "10.0.0.1/24" {
		t.Errorf("address = %q, want %q", arr[0].Get("address").String(), "10.0.0.1/24")
	}
}

func TestClient_Post_EmptyProplistAndQuery(t *testing.T) {
	ros := mock.NewRouterOS()
	ros.Seed("/ip/address",
		map[string]interface{}{"address": "10.0.0.1/24"},
	)
	client := NewMockClient(t, ros)

	result, err := client.Post("/ip/address/print", nil, nil, nil)
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	if !result.IsArray() {
		t.Fatal("expected array result")
	}
	if len(result.Array()) != 1 {
		t.Errorf("expected 1 result without filters, got %d", len(result.Array()))
	}
}

// ---------------------------------------------------------------------------
// Error handling tests
// ---------------------------------------------------------------------------

func TestClient_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(Error{
			Code:    400,
			Message: "bad request",
			Detail:  "invalid input",
		})
	})

	client := NewCustomHandlerClient(t, handler)
	_, err := client.Get("/test", nil)
	if err == nil {
		t.Fatal("expected error")
	}

	apiErr, ok := err.(Error)
	if !ok {
		t.Fatalf("expected Error, got %T: %v", err, err)
	}
	if apiErr.Code != 400 {
		t.Errorf("error Code = %d, want 400", apiErr.Code)
	}
	if apiErr.Message != "bad request" {
		t.Errorf("error Message = %q, want %q", apiErr.Message, "bad request")
	}
}

func TestClient_UnexpectedContentType(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>error</html>"))
	})

	client := NewCustomHandlerClient(t, handler)
	_, err := client.Get("/test", nil)
	if err == nil {
		t.Fatal("expected error for unexpected content type")
	}
}

func TestClient_ServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(Error{Code: 500, Message: "internal error", Detail: "something broke"})
	})

	client := NewCustomHandlerClient(t, handler)
	_, err := client.Get("/test", nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	apiErr, ok := err.(Error)
	if !ok {
		t.Fatalf("expected Error, got %T", err)
	}
	if apiErr.Code != 500 {
		t.Errorf("error Code = %d, want 500", apiErr.Code)
	}
}

func TestClient_EmptyResponseBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	client := NewCustomHandlerClient(t, handler)
	err := client.Delete("/ip/address/*1")
	if err != nil {
		t.Fatalf("Delete() with empty body error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sync tests (using mock RouterOS server for realistic state management)
// ---------------------------------------------------------------------------

func TestClient_Sync_CreatesNewEntries(t *testing.T) {
	ros := mock.NewRouterOS()
	client := NewMockClient(t, ros)

	reqs := []Request{
		{"address": "10.0.0.1/24", "interface": "ether1"},
	}
	results, err := client.Sync("/ip/address", reqs, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Get("\\.id").String() == "" {
		t.Error("expected .id in created resource")
	}

	// Verify resource is in the ros store
	resources := ros.Resources("/ip/address")
	if len(resources) != 1 {
		t.Fatalf("expected 1 stored resource, got %d", len(resources))
	}
}

func TestClient_Sync_ExistingMatchSkipsCreate(t *testing.T) {
	ros := mock.NewRouterOS()
	ros.Seed("/ip/address",
		map[string]interface{}{"address": "10.0.0.1/24", "interface": "ether1"},
	)
	client := NewMockClient(t, ros)

	reqs := []Request{
		{"address": "10.0.0.1/24", "interface": "ether1"},
	}
	results, err := client.Sync("/ip/address", reqs, nil, nil, nil, []string{".id"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Exact match should be kept and returned as result
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Get("address").String() != "10.0.0.1/24" {
		t.Errorf("address = %q, want %q", results[0].Get("address").String(), "10.0.0.1/24")
	}

	resources := ros.Resources("/ip/address")
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource (exact match should be kept), got %d", len(resources))
	}
}

func TestClient_Sync_PatchesExisting(t *testing.T) {
	ros := mock.NewRouterOS()
	ros.Seed("/ip/address",
		map[string]interface{}{"address": "10.0.0.1/24", "interface": "ether1"},
	)
	client := NewMockClient(t, ros)

	idFunc := func(r gjson.Result) string {
		return r.Get("interface").String()
	}

	reqs := []Request{
		{"address": "10.0.0.2/24", "interface": "ether1"},
	}
	results, err := client.Sync("/ip/address", reqs, nil, nil, idFunc, []string{".id"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Get("address").String() != "10.0.0.2/24" {
		t.Errorf("patched address = %q, want %q", results[0].Get("address").String(), "10.0.0.2/24")
	}
}

func TestClient_Sync_DeletesConflicts(t *testing.T) {
	ros := mock.NewRouterOS()
	ros.Seed("/ip/address",
		map[string]interface{}{"address": "192.168.1.1/24", "interface": "ether2"},
		map[string]interface{}{"address": "172.16.0.1/24", "interface": "ether3"},
	)
	client := NewMockClient(t, ros)

	reqs := []Request{
		{"address": "10.0.0.1/24", "interface": "ether1"},
	}
	_, err := client.Sync("/ip/address", reqs, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Conflicting entries should have been deleted, new one created
	resources := ros.Resources("/ip/address")
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource after sync, got %d", len(resources))
	}
	if resources[0]["address"] != "10.0.0.1/24" {
		t.Errorf("remaining resource address = %v, want 10.0.0.1/24", resources[0]["address"])
	}
}

func TestClient_Sync_NonArrayResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"unexpected string"`))
	})

	client := NewCustomHandlerClient(t, handler)
	_, err := client.Sync("/test", []Request{{"key": "value"}}, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for non-array response")
	}
}

func TestClient_Sync_WithQuery(t *testing.T) {
	ros := mock.NewRouterOS()
	ros.Seed("/ip/address",
		map[string]interface{}{"address": "192.168.1.1/24", "type": "dynamic"},
	)
	client := NewMockClient(t, ros)

	reqs := []Request{{"address": "10.0.0.1/24"}}
	_, err := client.Sync("/ip/address", reqs, Query{"type": "static"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// The query filter type=static means GET returns nothing matching,
	// so the desired entry is created. The type=dynamic one is untouched
	// because it didn't appear in the filtered GET.
	resources := ros.Resources("/ip/address")
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources (original + new), got %d", len(resources))
	}
}

func TestClient_Sync_MultipleRequests(t *testing.T) {
	ros := mock.NewRouterOS()
	client := NewMockClient(t, ros)

	reqs := []Request{
		{"address": "10.0.0.1/24", "interface": "ether1"},
		{"address": "10.0.0.2/24", "interface": "ether2"},
		{"address": "10.0.0.3/24", "interface": "ether3"},
	}
	results, err := client.Sync("/ip/address", reqs, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	resources := ros.Resources("/ip/address")
	if len(resources) != 3 {
		t.Fatalf("expected 3 stored resources, got %d", len(resources))
	}
}

// ---------------------------------------------------------------------------
// URL building
// ---------------------------------------------------------------------------

func TestClient_BuildURL(t *testing.T) {
	var receivedPaths []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPaths = append(receivedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	})

	client := NewCustomHandlerClient(t, handler)

	tests := []struct {
		name     string
		path     string
		wantPath string
	}{
		{"simple path", "/ip/address", "/rest/ip/address"},
		{"path without leading slash", "ip/address", "/rest/ip/address"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receivedPaths = nil
			client.Get(tt.path, nil)
			if len(receivedPaths) == 0 {
				t.Fatal("no request received")
			}
			if receivedPaths[0] != tt.wantPath {
				t.Errorf("path = %q, want %q", receivedPaths[0], tt.wantPath)
			}
		})
	}
}
