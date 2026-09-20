// Package ossplugin 为受信 OSS Sync 扩展提供公开 Go SDK。
package ossplugin

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

const ProtocolVersion = 1

type Registration struct {
	Hooks        []Hook         `json:"hooks,omitempty"`
	Routes       []Route        `json:"routes,omitempty"`
	Middleware   []Middleware   `json:"middleware,omitempty"`
	AdminPages   []AdminPage    `json:"admin_pages,omitempty"`
	Assets       []Asset        `json:"assets,omitempty"`
	Settings     []SettingField `json:"settings,omitempty"`
	Tasks        []Task         `json:"tasks,omitempty"`
	Migrations   []Migration    `json:"migrations,omitempty"`
	Dependencies []Dependency   `json:"dependencies,omitempty"`
	Lifecycle    Lifecycle      `json:"lifecycle"`
}

type Lifecycle struct {
	Activate   string `json:"activate,omitempty"`
	Deactivate string `json:"deactivate,omitempty"`
	Upgrade    string `json:"upgrade,omitempty"`
	Uninstall  string `json:"uninstall,omitempty"`
}

type Hook struct {
	Name     string `json:"name"`
	Callback string `json:"callback,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Priority int    `json:"priority,omitempty"`
	ID       string `json:"id,omitempty"`
	Label    string `json:"label,omitempty"`
}
type Route struct {
	Method   string `json:"method"`
	Path     string `json:"path"`
	Callback string `json:"callback,omitempty"`
	Auth     string `json:"auth,omitempty"`
	Priority int    `json:"priority,omitempty"`
}
type Middleware struct {
	Name       string `json:"name"`
	Callback   string `json:"callback,omitempty"`
	Stage      string `json:"stage"`
	PathPrefix string `json:"path_prefix,omitempty"`
	Priority   int    `json:"priority,omitempty"`
}
type AdminPage struct {
	Slug     string `json:"slug"`
	Label    string `json:"label"`
	Callback string `json:"callback,omitempty"`
	Parent   string `json:"parent,omitempty"`
	Position int    `json:"position,omitempty"`
}
type Asset struct {
	Path string `json:"path"`
	URL  string `json:"url,omitempty"`
}
type SettingField struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Type      string `json:"type"`
	MaxLength int    `json:"max_length,omitempty"`
}
type Task struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Callback string `json:"callback,omitempty"`
}
type Migration struct {
	ID         string   `json:"id"`
	Statements []string `json:"statements"`
}
type Dependency struct {
	PluginID   string `json:"plugin_id"`
	Constraint string `json:"constraint,omitempty"`
}

type Request struct {
	Method     string              `json:"method"`
	Path       string              `json:"path"`
	Callback   string              `json:"callback,omitempty"`
	Query      map[string][]string `json:"query,omitempty"`
	Params     map[string]string   `json:"params,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Cookies    map[string]string   `json:"cookies,omitempty"`
	User       *User               `json:"user,omitempty"`
	Settings   map[string]any      `json:"settings,omitempty"`
	Hook       string              `json:"hook,omitempty"`
	Payload    map[string]any      `json:"payload,omitempty"`
	BodyBase64 string              `json:"body_base64,omitempty"`
}

type User struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type Response struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers,omitempty"`
	BodyBase64 string            `json:"body_base64,omitempty"`
}
type Handler func(context.Context, Request) (Response, error)

type ServiceClient struct{ client *Client }
type QueryRow map[string]any
type ExecResult struct {
	RowsAffected int64 `json:"rows_affected"`
}
type PluginSummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Runtime string `json:"runtime"`
	Enabled bool   `json:"enabled"`
}

func (c *Client) Services() ServiceClient { return ServiceClient{client: c} }

func (s ServiceClient) Query(ctx context.Context, query string, args ...any) ([]QueryRow, error) {
	var rows []QueryRow
	err := s.client.HostCall(ctx, "db.query", map[string]any{"query": query, "args": args}, &rows)
	return rows, err
}

func (s ServiceClient) Exec(ctx context.Context, query string, args ...any) (ExecResult, error) {
	var result ExecResult
	err := s.client.HostCall(ctx, "db.exec", map[string]any{"query": query, "args": args}, &result)
	return result, err
}

func (s ServiceClient) Models(ctx context.Context) ([]string, error) {
	var result []string
	err := s.client.HostCall(ctx, "host.models", map[string]any{}, &result)
	return result, err
}

func (s ServiceClient) ModelList(ctx context.Context, model string, limit int) (any, error) {
	var result any
	err := s.client.HostCall(ctx, "host.model.list", map[string]any{"model": model, "limit": limit}, &result)
	return result, err
}

func (s ServiceClient) Create(ctx context.Context, model string, values map[string]any) (ExecResult, error) {
	var result ExecResult
	err := s.client.HostCall(ctx, "host.model.create", map[string]any{"model": model, "values": values}, &result)
	return result, err
}

func (s ServiceClient) Update(ctx context.Context, model string, where, values map[string]any) (ExecResult, error) {
	var result ExecResult
	err := s.client.HostCall(ctx, "host.model.update", map[string]any{"model": model, "where": where, "values": values}, &result)
	return result, err
}

func (s ServiceClient) Delete(ctx context.Context, model string, where map[string]any) (ExecResult, error) {
	var result ExecResult
	err := s.client.HostCall(ctx, "host.model.delete", map[string]any{"model": model, "where": where}, &result)
	return result, err
}

func (s ServiceClient) Plugins(ctx context.Context) ([]PluginSummary, error) {
	var result []PluginSummary
	err := s.client.HostCall(ctx, "host.plugin.list", map[string]any{}, &result)
	return result, err
}

func (s ServiceClient) GetSetting(ctx context.Context, vaultID string, result any) error {
	return s.client.HostCall(ctx, "host.settings.get", map[string]any{"vault_id": vaultID}, result)
}

func (s ServiceClient) SetSetting(ctx context.Context, vaultID string, config any) error {
	var result map[string]bool
	return s.client.HostCall(ctx, "host.settings.set", map[string]any{"vault_id": vaultID, "config": config}, &result)
}

func (s ServiceClient) Hook(ctx context.Context, name string, payload any, result any) error {
	return s.client.HostCall(ctx, "host.hook", map[string]any{"name": name, "payload": payload}, result)
}

type Client struct {
	in        *bufio.Scanner
	out       *bufio.Writer
	handlers  map[string]Handler
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan frame
	nextID    uint64
	wait      sync.WaitGroup
}

func New(in io.Reader, out io.Writer) *Client {
	return &Client{
		in: newScanner(in), out: bufio.NewWriter(out),
		handlers: make(map[string]Handler), pending: make(map[string]chan frame),
	}
}

func newScanner(in io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	return scanner
}

func (c *Client) On(callback string, handler Handler) error {
	if callback == "" || handler == nil {
		return errors.New("callback and handler are required")
	}
	c.handlers[callback] = handler
	return nil
}

func (c *Client) Run(registration Registration) error {
	if err := c.write(frame{Type: "ready", APIVersion: ProtocolVersion, Registration: &registration}); err != nil {
		return fmt.Errorf("write ready frame: %w", err)
	}
	for c.in.Scan() {
		var incoming frame
		if err := json.Unmarshal(c.in.Bytes(), &incoming); err != nil {
			return fmt.Errorf("decode host frame: %w", err)
		}
		switch incoming.Type {
		case "shutdown":
			c.wait.Wait()
			return nil
		case "host_response", "host_error":
			c.finishHostCall(incoming)
		case "request":
			if incoming.ID == "" {
				return errors.New("host request has no id")
			}
			c.wait.Add(1)
			go func(call frame) {
				defer c.wait.Done()
				c.handleRequest(call)
			}(incoming)
		default:
			return fmt.Errorf("unsupported host frame %q", incoming.Type)
		}
	}
	err := c.in.Err()
	failure := err
	if failure == nil {
		failure = io.EOF
	}
	c.failHostCalls(failure)
	c.wait.Wait()
	return err
}

func (c *Client) handleRequest(incoming frame) {
	var request Request
	if err := json.Unmarshal(incoming.Request, &request); err != nil {
		_ = c.write(frame{Type: "error", ID: incoming.ID, Error: err.Error()})
		return
	}
	handler := c.handlers[request.Callback]
	if handler == nil {
		handler = c.handlers[request.Method+":"+request.Path]
	}
	if handler == nil {
		_ = c.write(frame{Type: "error", ID: incoming.ID, Error: "callback is not registered"})
		return
	}
	response, err := handler(context.Background(), request)
	if err != nil {
		_ = c.write(frame{Type: "error", ID: incoming.ID, Error: err.Error()})
		return
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		_ = c.write(frame{Type: "error", ID: incoming.ID, Error: err.Error()})
		return
	}
	_ = c.write(frame{Type: "response", ID: incoming.ID, Response: encoded})
}

func (c *Client) WriteTextResponse(status int, text string) Response {
	return Response{Status: status, Headers: map[string]string{"Content-Type": "text/plain; charset=utf-8"}, BodyBase64: base64.StdEncoding.EncodeToString([]byte(text))}
}

func (c *Client) HostCall(ctx context.Context, method string, params any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode host parameters: %w", err)
	}
	id := fmt.Sprintf("host-%d", c.nextHostID())
	responseCh := make(chan frame, 1)
	c.pendingMu.Lock()
	c.pending[id] = responseCh
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()
	if err := c.write(frame{Type: "host_call", ID: id, Method: method, Params: encoded}); err != nil {
		return fmt.Errorf("write host call: %w", err)
	}
	select {
	case response := <-responseCh:
		if response.Type == "host_error" {
			return errors.New(response.Error)
		}
		if response.Type != "host_response" {
			return fmt.Errorf("unexpected host response %q", response.Type)
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode host result: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) nextHostID() uint64 {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.nextID++
	return c.nextID
}

func (c *Client) write(value frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := json.NewEncoder(c.out).Encode(value); err != nil {
		return err
	}
	return c.out.Flush()
}

func (c *Client) finishHostCall(response frame) {
	c.pendingMu.Lock()
	responseCh := c.pending[response.ID]
	c.pendingMu.Unlock()
	if responseCh != nil {
		responseCh <- response
	}
}

func (c *Client) failHostCalls(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, responseCh := range c.pending {
		responseCh <- frame{Type: "host_error", ID: id, Error: err.Error()}
	}
}

type frame struct {
	Type         string          `json:"type"`
	ID           string          `json:"id,omitempty"`
	APIVersion   int             `json:"api_version,omitempty"`
	Request      json.RawMessage `json:"request,omitempty"`
	Response     json.RawMessage `json:"response,omitempty"`
	Error        string          `json:"error,omitempty"`
	Method       string          `json:"method,omitempty"`
	Params       json.RawMessage `json:"params,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
	Registration *Registration   `json:"registration,omitempty"`
}

func RunMain(registration Registration, configure func(*Client) error) {
	client := New(os.Stdin, os.Stdout)
	if err := configure(client); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := client.Run(registration); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
