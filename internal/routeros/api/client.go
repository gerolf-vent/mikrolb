package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-logr/logr"
	"github.com/tidwall/gjson"
)

type Request map[string]any

type Query map[string]string

type FilterFunc func(gjson.Result) bool

type IDFunc func(gjson.Result) string

// HTTPRuntime is the interface for performing HTTP requests.
// *http.Client satisfies this interface.
type HTTPRuntime interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	endpoint   *url.URL
	username   string
	password   string
	logger     logr.Logger
	httpClient HTTPRuntime
}

func NewClient(endpoint *url.URL, tlsConfig *tls.Config, logger logr.Logger) *Client {
	return &Client{
		endpoint: &url.URL{
			Scheme: endpoint.Scheme,
			Host:   endpoint.Host,
			Path:   fmt.Sprintf("%s/rest", strings.TrimSuffix(endpoint.Path, "/")),
		},
		logger: logger,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	}
}

func (c *Client) SetHTTPClient(runtime HTTPRuntime) {
	c.httpClient = runtime
}

func (c *Client) SetCredentials(username, password string) {
	c.username = username
	c.password = password
}

func (c *Client) Get(path string, query Query) (gjson.Result, error) {
	c.logger.V(2).Info("API request", "method", "GET", "path", path, "query", query)
	return c.doRequest("GET", path, nil, query)
}

func (c *Client) Put(path string, req Request) (gjson.Result, error) {
	c.logger.V(2).Info("API request", "method", "PUT", "path", path, "request", req)
	return c.doRequest("PUT", path, req, nil)
}

func (c *Client) Patch(path string, req Request) (gjson.Result, error) {
	c.logger.V(2).Info("API request", "method", "PATCH", "path", path, "request", req)
	return c.doRequest("PATCH", path, req, nil)
}

func (c *Client) Delete(path string) error {
	c.logger.V(2).Info("API request", "method", "DELETE", "path", path)
	_, err := c.doRequest("DELETE", path, nil, nil)
	return err
}

func (c *Client) Post(path string, req Request, proplist, query []string) (gjson.Result, error) {
	c.logger.V(2).Info("API request", "method", "POST", "path", path, "proplist", proplist, "query", query)
	if req == nil {
		req = make(Request)
	}
	if len(proplist) > 0 {
		req[".proplist"] = proplist
	}
	if len(query) > 0 {
		req[".query"] = query
	}
	return c.doRequest("POST", path, req, nil)
}

func (c *Client) Sync(path string, reqs []Request, query Query, filterFunc FilterFunc, idFunc IDFunc, ignoredFields []string) ([]gjson.Result, error) {
	// Pre-marshal requests to gjson.Result for easier comparison later
	reqObjs := make([]gjson.Result, len(reqs))
	for i, req := range reqs {
		objJSON, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request %d: %w", i, err)
		}
		reqObjs[i] = gjson.ParseBytes(objJSON)
	}

	// Get existing entries
	resp, err := c.Get(path, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing entries: %w", err)
	}

	// Prepare results
	results := make([]gjson.Result, len(reqs))

	// Check response format
	if !resp.IsArray() {
		return nil, fmt.Errorf("unexpected response format: expected array, got %s", resp.Type.String())
	}

	found := make([]*gjson.Result, len(reqs))
	patchable := make([]*gjson.Result, len(reqs))
	var conflicts []*gjson.Result

	// Match existing objects to requests
	for _, obj := range resp.Array() {
		if filterFunc != nil && !filterFunc(obj) {
			continue // Skip objects that don't match the filter
		}

		matched := false
		for j, reqObj := range reqObjs {
			if found[j] == nil && compareObjs(obj, reqObj, ignoredFields) {
				found[j] = &obj
				matched = true
				break
			}
			if patchable[j] == nil && (idFunc == nil || idFunc(obj) == idFunc(reqObj)) {
				patchable[j] = &obj
				matched = true
				break
			}
		}
		if !matched {
			conflicts = append(conflicts, &obj)
		}
	}

	// Determine whether the patchable objects are useful
	for i, obj := range patchable {
		if found[i] != nil && obj != nil {
			// Treat as conflict if there is an exact match (found),
			// since we don't need to patch when an exact match exists.
			conflicts = append(conflicts, obj)
			patchable[i] = nil
		}
	}

	// Clean up any conflicting objects
	for _, obj := range conflicts {
		id := obj.Get("\\.id").String()
		if id == "" {
			c.logger.Info("skipping object without .id field during sync", "object", obj.String())
			continue
		}
		if err := c.Delete(fmt.Sprintf("%s/%s", path, id)); err != nil {
			return nil, fmt.Errorf("failed to delete conflicting object with id %s: %w", id, err)
		}
	}

	// Create any missing objects or patch existing ones
	for i, req := range reqs {
		if found[i] == nil {
			if patchable[i] != nil {
				id := patchable[i].Get("\\.id").String()
				if id != "" {
					patchReq := generatePatchRequest(*patchable[i], reqObjs[i], ignoredFields)
					resp, err := c.Patch(fmt.Sprintf("%s/%s", path, id), patchReq)
					if err != nil {
						return nil, fmt.Errorf("failed to patch existing object with id %s for request %d: %w", id, i, err)
					}
					results[i] = resp
					continue
				}
				c.logger.V(2).Info("patchable object missing .id field, treating as new object", "object", patchable[i].String())
			}
			resp, err := c.Put(path, req)
			if err != nil {
				return nil, fmt.Errorf("failed to create missing object for request %d: %w", i, err)
			}
			results[i] = resp
		} else {
			results[i] = *found[i]
		}
	}

	return results, nil
}

func (c *Client) buildURL(path string, query Query) string {
	url := &url.URL{
		Scheme: c.endpoint.Scheme,
		Host:   c.endpoint.Host,
		Path:   strings.TrimSuffix(fmt.Sprintf("%s/%s", c.endpoint.Path, strings.TrimPrefix(path, "/")), "/"),
	}

	q := url.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	url.RawQuery = q.Encode()

	urlStr := url.Scheme + "://" + url.Host + url.Path
	if url.RawQuery != "" {
		urlStr += "?" + url.RawQuery
	}

	return urlStr
}

func (c *Client) doRequest(method, path string, req Request, query Query) (gjson.Result, error) {
	url := c.buildURL(path, query)

	var reqBody io.Reader
	if req != nil {
		jsonBody, err := json.Marshal(req)
		if err != nil {
			return gjson.Result{}, fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	return c.call(method, url, reqBody)
}

func (c *Client) call(method string, url string, body io.Reader) (gjson.Result, error) {
	logger := c.logger.WithValues("method", method, "url", url)

	httpReq, err := http.NewRequest(method, url, body)
	if err != nil {
		return gjson.Result{}, err
	}
	if c.username != "" && c.password != "" {
		httpReq.SetBasicAuth(c.username, c.password)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		logger.V(2).Error(err, "API request failed")
		return gjson.Result{}, err
	}
	defer httpResp.Body.Close()

	logger = logger.WithValues("status", httpResp.StatusCode)

	httpRespContentType := httpResp.Header.Get("Content-Type")
	if httpRespContentType != "" && httpRespContentType != "application/json" {
		logger.V(2).Error(nil, "API returned unexpected content type", "contentType", httpRespContentType)
		return gjson.Result{}, fmt.Errorf("unexpected content type of response: %s", httpRespContentType)
	}

	if httpResp.StatusCode >= 400 {
		var apiErr Error
		if err := json.NewDecoder(httpResp.Body).Decode(&apiErr); err != nil {
			logger.V(2).Error(err, "API returned unknown error format")
			return gjson.Result{}, fmt.Errorf("API error: status %d, failed to decode error response: %w", httpResp.StatusCode, err)
		}
		logger.V(2).Error(nil, "API returned error", "code", apiErr.Code, "message", apiErr.Message, "detail", apiErr.Detail)
		return gjson.Result{}, apiErr
	}

	httpRespBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		logger.V(2).Error(err, "failed to read API response body")
		return gjson.Result{}, fmt.Errorf("failed to read response body: %w", err)
	}

	if logger.GetV() >= 3 {
		logger.V(3).Info("API request succeeded", "body", string(httpRespBody))
	} else {
		logger.V(2).Info("API request succeeded")
	}

	return gjson.ParseBytes(httpRespBody), nil
}
