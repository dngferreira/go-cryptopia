// Package cryptopia is the unofficial client to access to Cryptopia API
package cryptopia

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	// PublicAPIEndpoint is the Cryptopia Public API endpoint.
	PublicAPIEndpoint = "https://www.cryptopia.co.nz/api/"
)

// Client manages all the communication with the Cryptopia API.
type Client struct {
	// Base URL for Public API requests.
	BaseURL *url.URL

	// Auth data
	APIKey    string
	APISecret string
}

// NewClient creates new Cryptopia API client.
func NewClient() *Client {
	baseURL, _ := url.Parse(PublicAPIEndpoint)
	return &Client{BaseURL: baseURL}
}

// newRequest create new API request. Relative url can be provided in refURL.
func (c *Client) newRequest(ctx context.Context, method string, refURL string, params url.Values) (*http.Request, error) {
	rel, err := url.Parse(refURL)
	if err != nil {
		return nil, err
	}
	if params != nil {
		rel.RawQuery = params.Encode()
	}
	var req *http.Request
	u := c.BaseURL.ResolveReference(rel)
	req, err = http.NewRequest(method, u.String(), nil)

	if err != nil {
		return nil, err
	}

	req = req.WithContext(ctx)

	return req, nil
}

// newAuthenticatedRequest creates new http request for authenticated routes.
func (c *Client) newAuthenticatedRequest(ctx context.Context, method string, params map[string]interface{}) (*http.Request, error) {
	if params == nil {
		params = make(map[string]interface{})
	}

	jsonBody, err := json.Marshal(params)
	if err != nil {
		return nil, errors.Wrap(err, "Faild to marshal params to json")
	}

	var req *http.Request
	uri := *c.BaseURL
	uri.Path = path.Join(c.BaseURL.Path, method)
	req, err = http.NewRequest("POST", uri.String(), bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, errors.Wrap(err, "Faild to new request")
	}

	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
	signature := c.APIKey +
		"POST" +
		strings.ToLower(url.QueryEscape(uri.String())) +
		nonce +
		c.md5base64(jsonBody)

	hmacSignature := c.sha256base64(signature)
	auth := "amx " +
		c.APIKey +
		":" +
		hmacSignature +
		":" +
		nonce

	req.Header.Add("Content-Type", "application/json; charset=utf-8")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", auth)

	req = req.WithContext(ctx)

	return req, nil
}

func (c *Client) md5base64(b []byte) string {
	md5 := md5.Sum(b)
	return base64.StdEncoding.EncodeToString(md5[:])
}

func (c *Client) sha256base64(s string) string {
	key, _ := base64.StdEncoding.DecodeString(c.APISecret)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(s))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// Auth sets api key and secret for usage is requests that requires authentication.
func (c *Client) Auth(key string, secret string) *Client {
	c.APIKey = key
	c.APISecret = secret

	return c
}

var httpDo = func(req *http.Request) (*http.Response, error) {
	return http.DefaultClient.Do(req)
}

// Do executes API request created by NewRequest method or custom *http.Request.
func (c *Client) do(req *http.Request, v interface{}) (*Response, error) {
	resp, err := httpDo(req)

	if err != nil {
		return nil, errors.Wrap(err, "")
	}
	defer resp.Body.Close()

	response := newResponse(resp)

	err = checkResponse(response)
	if err != nil {
		// Return response in case caller need to debug it.
		return response, errors.Wrap(err, "")
	}

	if v == nil {
		return response, nil
	}

	apiRes := APIResponse{}
	err = json.Unmarshal(response.Body, &apiRes)
	if err != nil {
		return response, errors.Wrap(err, "Faild to unmarshal response body to json")
	}
	if apiRes.Error != "" {
		return response, &ErrorResponse{Response: response, Message: "Error: " + apiRes.Error}
	}

	err = json.Unmarshal(apiRes.Data, v)
	if err != nil {
		return response, errors.Wrap(err, "")
	}

	return response, nil
}

// APIResponse is the API's response base json format.
type APIResponse struct {
	Success bool
	Message string
	Data    json.RawMessage
	Error   string
}

// Response is wrapper for standard http.Response and provides
// more methods.
type Response struct {
	Response *http.Response
	Body     []byte
}

// newResponse creates new wrapper.
func newResponse(r *http.Response) *Response {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		body = []byte(`Error reading body:` + err.Error())
	}

	return &Response{r, body}
}

// String converts response body to string.
// An empty string will be returned if error.
func (r *Response) String() string {
	return string(r.Body)
}

// ErrorResponse is the custom error type that is returned if the API returns an
// error.
type ErrorResponse struct {
	Response *Response
	Message  string `json:"error"`
}

func (r *ErrorResponse) Error() string {
	return fmt.Sprintf("%v %v: %d %v",
		r.Response.Response.Request.Method,
		r.Response.Response.Request.URL,
		r.Response.Response.StatusCode,
		r.Message,
	)
}

// checkResponse checks response status code and response
// for errors.
func checkResponse(r *Response) error {
	if c := r.Response.StatusCode; 200 <= c && c <= 299 {
		return nil
	}

	// Try to decode error message
	errorResponse := &ErrorResponse{Response: r}
	err := json.Unmarshal(r.Body, errorResponse)
	if err != nil {
		errorResponse.Message = "Error decoding response error message. " +
			"Please see response body for more information."
	}

	return errorResponse
}
