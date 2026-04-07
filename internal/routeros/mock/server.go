package mock

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// RouterOSResource represents a single resource stored in the mock RouterOS API.
type RouterOSResource struct {
	ID     string
	Fields map[string]interface{}
}

// RouterOS is an in-memory fake of the RouterOS REST API.
// It supports the CRUD operations used by the Client:
//
//	GET    /rest/<path>        → list all resources under <path>
//	GET    /rest/<path>/<id>   → get a single resource
//	PUT    /rest/<path>        → create a new resource (returns generated .id)
//	PATCH  /rest/<path>/<id>   → update an existing resource
//	DELETE /rest/<path>/<id>   → delete a resource
//	POST   /rest/<path>/print  → print with .proplist / .query filters
//
// Resources are stored per-path (e.g. "/ip/address") and auto-assigned
// incrementing IDs like "*1", "*2", etc.
type RouterOS struct {
	mu        sync.Mutex
	resources map[string][]RouterOSResource // path → resources
	requests  []map[string]interface{}
	nextID    int
}

// NewRouterOS creates a fresh mock with no resources.
func NewRouterOS() *RouterOS {
	return &RouterOS{
		resources: make(map[string][]RouterOSResource),
		requests:  make([]map[string]interface{}, 0),
		nextID:    1,
	}
}

// Seed pre-populates resources at the given path (e.g. "/ip/address").
// Each map becomes a resource with an auto-generated .id.
func (m *RouterOS) Seed(path string, entries ...map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range entries {
		id := fmt.Sprintf("*%d", m.nextID)
		m.nextID++
		fields := copyMap(entry)
		fields[".id"] = id
		m.resources[path] = append(m.resources[path], RouterOSResource{ID: id, Fields: fields})
	}
}

// Handler returns an http.Handler suitable for httptest.NewServer.
func (m *RouterOS) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_state" {
			m.handleState(w, r)
			return
		}

		// Strip the /rest prefix that the Client adds.
		path := strings.TrimPrefix(r.URL.Path, "/rest")
		path = strings.TrimSuffix(path, "/")

		switch r.Method {
		case http.MethodGet:
			m.handleGet(w, r, path)
		case http.MethodPut:
			m.handlePut(w, r, path)
		case http.MethodPatch:
			m.handlePatch(w, r, path)
		case http.MethodDelete:
			m.handleDelete(w, r, path)
		case http.MethodPost:
			m.handlePost(w, r, path)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

// handleGet returns a single resource or a list.
func (m *RouterOS) handleGet(w http.ResponseWriter, r *http.Request, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = append(m.requests, map[string]interface{}{
		"method": "GET",
		"path":   path,
		"query":  r.URL.Query(),
	})

	// Check if the path ends with an ID (e.g. /ip/address/*1)
	basePath, id := splitID(path)
	if id != "" {
		// Single-resource fetch
		for _, res := range m.resources[basePath] {
			if res.ID == id {
				writeJSON(w, http.StatusOK, res.Fields)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, Error{Code: 404, Message: "not found", Detail: "no such item"})
		return
	}

	// List resources, applying query-string filters.
	entries := m.resources[path]
	result := filterByQuery(entries, r.URL.Query())

	out := make([]map[string]interface{}, 0, len(result))
	for _, res := range result {
		out = append(out, res.Fields)
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePut creates a new resource.
func (m *RouterOS) handlePut(w http.ResponseWriter, r *http.Request, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	bodyRaw, err := io.ReadAll(r.Body)
	if err != nil {
		m.requests = append(m.requests, map[string]interface{}{
			"method": "PUT",
			"path":   path,
			"body":   nil,
			"error":  err.Error(),
		})

		writeJSON(w, http.StatusBadRequest, Error{Code: 400, Message: "bad request", Detail: err.Error()})
		return
	}

	var body map[string]interface{}
	if err := json.Unmarshal(bodyRaw, &body); err != nil {
		m.requests = append(m.requests, map[string]interface{}{
			"method":  "PUT",
			"path":    path,
			"bodyRaw": bodyRaw,
			"error":   err.Error(),
		})

		writeJSON(w, http.StatusBadRequest, Error{Code: 400, Message: "bad request", Detail: err.Error()})
		return
	}

	m.requests = append(m.requests, map[string]interface{}{
		"method": "PUT",
		"path":   path,
		"body":   body,
	})

	id := fmt.Sprintf("*%d", m.nextID)
	m.nextID++
	fields := copyMap(body)
	fields[".id"] = id
	m.resources[path] = append(m.resources[path], RouterOSResource{ID: id, Fields: fields})

	writeJSON(w, http.StatusCreated, fields)
}

// handlePatch updates an existing resource.
func (m *RouterOS) handlePatch(w http.ResponseWriter, r *http.Request, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	basePath, id := splitID(path)
	if id == "" {
		m.requests = append(m.requests, map[string]interface{}{
			"method": "PATCH",
			"path":   path,
			"error":  "missing resource id",
		})

		writeJSON(w, http.StatusBadRequest, Error{Code: 400, Message: "bad request", Detail: "missing resource id"})
		return
	}

	bodyRaw, err := io.ReadAll(r.Body)
	if err != nil {
		m.requests = append(m.requests, map[string]interface{}{
			"method": "PATCH",
			"path":   path,
			"body":   nil,
			"error":  err.Error(),
		})

		writeJSON(w, http.StatusBadRequest, Error{Code: 400, Message: "bad request", Detail: err.Error()})
		return
	}

	var body map[string]interface{}
	if err := json.Unmarshal(bodyRaw, &body); err != nil {
		m.requests = append(m.requests, map[string]interface{}{
			"method":  "PATCH",
			"path":    path,
			"bodyRaw": bodyRaw,
			"error":   err.Error(),
		})

		writeJSON(w, http.StatusBadRequest, Error{Code: 400, Message: "bad request", Detail: err.Error()})
		return
	}

	m.requests = append(m.requests, map[string]interface{}{
		"method": "PATCH",
		"path":   path,
		"body":   body,
	})

	for i, res := range m.resources[basePath] {
		if res.ID == id {
			for k, v := range body {
				if v == nil {
					delete(m.resources[basePath][i].Fields, k)
				} else {
					m.resources[basePath][i].Fields[k] = v
				}
			}
			writeJSON(w, http.StatusOK, m.resources[basePath][i].Fields)
			return
		}
	}

	writeJSON(w, http.StatusNotFound, Error{Code: 404, Message: "not found", Detail: "no such item"})
}

// handleDelete removes a resource.
func (m *RouterOS) handleDelete(w http.ResponseWriter, _ *http.Request, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	basePath, id := splitID(path)
	if id == "" {
		m.requests = append(m.requests, map[string]interface{}{
			"method": "DELETE",
			"path":   path,
			"error":  "missing resource id",
		})

		writeJSON(w, http.StatusBadRequest, Error{Code: 400, Message: "bad request", Detail: "missing resource id"})
		return
	}

	m.requests = append(m.requests, map[string]interface{}{
		"method": "DELETE",
		"path":   path,
	})

	entries := m.resources[basePath]
	for i, res := range entries {
		if res.ID == id {
			m.resources[basePath] = append(entries[:i], entries[i+1:]...)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	writeJSON(w, http.StatusNotFound, Error{Code: 404, Message: "not found", Detail: "no such item"})
}

// handlePost handles POST /rest/<path>/print with .proplist and .query filters.
func (m *RouterOS) handlePost(w http.ResponseWriter, r *http.Request, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// POST is used for /print commands — strip the /print suffix for lookup
	basePath := strings.TrimSuffix(path, "/print")

	bodyRaw, err := io.ReadAll(r.Body)
	if err != nil {
		m.requests = append(m.requests, map[string]interface{}{
			"method": "POST",
			"path":   path,
			"body":   nil,
			"error":  err.Error(),
		})

		writeJSON(w, http.StatusBadRequest, Error{Code: 400, Message: "bad request", Detail: err.Error()})
		return
	}

	var body map[string]interface{}
	if err := json.Unmarshal(bodyRaw, &body); err != nil {
		m.requests = append(m.requests, map[string]interface{}{
			"method":  "POST",
			"path":    path,
			"bodyRaw": bodyRaw,
			"error":   err.Error(),
		})

		writeJSON(w, http.StatusBadRequest, Error{Code: 400, Message: "bad request", Detail: err.Error()})
		return
	}

	m.requests = append(m.requests, map[string]interface{}{
		"method": "POST",
		"path":   path,
		"body":   body,
	})

	entries := m.resources[basePath]

	// Apply .query filters (e.g. ["interface=ether1"])
	if rawQuery, ok := body[".query"]; ok {
		if queryItems, ok := rawQuery.([]interface{}); ok {
			filtered := make([]RouterOSResource, 0)
			for _, res := range entries {
				if matchesQuery(res, queryItems) {
					filtered = append(filtered, res)
				}
			}
			entries = filtered
		}
	}

	// Apply .proplist projection
	var proplist []string
	if rawProplist, ok := body[".proplist"]; ok {
		if propItems, ok := rawProplist.([]interface{}); ok {
			for _, p := range propItems {
				if s, ok := p.(string); ok {
					proplist = append(proplist, s)
				}
			}
		}
	}

	out := make([]map[string]interface{}, 0, len(entries))
	for _, res := range entries {
		if len(proplist) > 0 {
			projected := make(map[string]interface{})
			for _, key := range proplist {
				if v, ok := res.Fields[key]; ok {
					projected[key] = v
				}
			}
			out = append(out, projected)
		} else {
			out = append(out, res.Fields)
		}
	}

	writeJSON(w, http.StatusOK, out)
}

// Resources returns a snapshot of all resources at the given path.
func (m *RouterOS) Resources(path string) []map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.resources[path]
	out := make([]map[string]interface{}, len(entries))
	for i, res := range entries {
		out[i] = copyMap(res.Fields)
	}
	return out
}

// --- helpers ---

// splitID separates "/ip/address/*1" into ("/ip/address", "*1").
// If no ID segment is present, id is "".
func splitID(path string) (basePath, id string) {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path, ""
	}
	tail := path[idx+1:]
	if strings.HasPrefix(tail, "*") {
		return path[:idx], tail
	}
	return path, ""
}

func filterByQuery(entries []RouterOSResource, query url.Values) []RouterOSResource {
	if len(query) == 0 {
		return entries
	}
	var out []RouterOSResource
	for _, res := range entries {
		match := true
		for key, vals := range query {
			fieldVal, _ := res.Fields[key].(string)
			if fieldVal != vals[0] {
				match = false
				break
			}
		}
		if match {
			out = append(out, res)
		}
	}
	return out
}

func matchesQuery(res RouterOSResource, queryItems []interface{}) bool {
	for _, item := range queryItems {
		s, ok := item.(string)
		if !ok {
			continue
		}
		parts := strings.SplitN(s, "=", 2)
		if len(parts) != 2 {
			continue
		}
		fieldVal, _ := res.Fields[parts[0]].(string)
		if fieldVal != parts[1] {
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	c := make(map[string]interface{}, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// State returns a copy of the current resources stored in the mock server.
func (m *RouterOS) State() map[string][]RouterOSResource {
	m.mu.Lock()
	defer m.mu.Unlock()

	state := make(map[string][]RouterOSResource)
	for k, v := range m.resources {
		copied := make([]RouterOSResource, len(v))
		copy(copied, v)
		state[k] = copied
	}
	return state
}

// handleState serves the current requests and state as JSON.
func (m *RouterOS) handleState(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state := make(map[string][]RouterOSResource)
	for k, v := range m.resources {
		copied := make([]RouterOSResource, len(v))
		copy(copied, v)
		state[k] = copied
	}

	reqs := make([]map[string]interface{}, len(m.requests))
	copy(reqs, m.requests)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"requests": reqs,
		"state":    state,
	})
}
