// Package mcpclient connects approved remote MCP services to the tool executor.
package mcpclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/catalog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/credential"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/quota"
)

type Tool struct {
	ID          core.ID
	Name        string
	Description string
	InputSchema json.RawMessage
	Enabled     bool
	Problem     string
}
type Connection struct {
	ID             core.ID
	OwnerID        core.ID
	Name           string
	Endpoint       string
	Auth           string
	Header         string
	CredentialID   core.ID
	Enabled        bool
	Deleted        bool
	Version        core.Version
	CallLimit      int64
	Concurrent     int
	TimeoutSeconds int
	Tools          []Tool
	CheckedAt      *time.Time
	Status         string
}
type View struct {
	ID             core.ID
	Name           string
	Endpoint       string
	Auth           string
	Header         string
	Enabled        bool
	Version        core.Version
	CallLimit      int64
	Concurrent     int
	TimeoutSeconds int
	Tools          []Tool
	CheckedAt      *time.Time
	Status         string
	Configured     bool
	Calls          int64
	Active         int
}
type SaveRequest struct {
	ID                           core.ID
	Name, Endpoint, Auth, Header string
	Enabled                      bool
	EnabledTools                 []core.ID
	APIKey                       core.Secret
	Verification                 auth.Verification
	Mutation                     core.Mutation
	CallLimit                    int64
	Concurrent, TimeoutSeconds   int
}
type Request struct {
	ID           core.ID
	Mutation     core.Mutation
	Verification auth.Verification
}
type Options struct {
	Authorize            func(context.Context, core.Principal, bool) error
	IsAdministrator      func(context.Context, core.Principal) (bool, error)
	CheckConnection      func(context.Context, core.Principal, core.ID) error
	CheckTool            func(context.Context, core.Principal, core.ID) error
	CheckConnectionQuota func(context.Context, core.Principal, core.ID) error
	Credentials          credential.Manager
	Broker               credential.Broker
	HTTPClient           func(context.Context, core.Principal, core.Execution, core.ID, string) (*http.Client, error)
}
type running struct {
	Call      CallRecord
	Principal core.Principal
	Cancel    context.CancelFunc
}
type Service struct {
	store   persistence.Store
	options Options
	mu      sync.Mutex
	runs    map[string]running
}

func New(store persistence.Store, options Options) *Service {
	return &Service{store: store, options: options, runs: map[string]running{}}
}
func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func newID() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID(hex.EncodeToString(b[:]))
}
func invalid(message string) error { return core.Invalid("mcp", message) }
func denied() error                { return core.NewError(core.Forbidden, "MCP connection or tool unavailable") }
func conflict() error {
	return core.NewError(core.Conflict, "MCP connection changed; reload before saving")
}
func (s *Service) authorize(ctx context.Context, p core.Principal, write bool) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if s.options.Authorize == nil {
		return denied()
	}
	return s.options.Authorize(ctx, p, write)
}
func (c Connection) Origin() string {
	u, _ := url.Parse(c.Endpoint)
	if u == nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
func (s *Service) connection(ctx context.Context, owner, id core.ID) (Connection, error) {
	return catalog.Load[Connection](ctx, s.store, "mcp.connections", id)
}
func (s *Service) rows(ctx context.Context, owner core.ID) ([]Connection, error) {
	return catalog.Select[Connection](ctx, s.store, "mcp.connections", persistence.Query{Where: []persistence.Filter{persistence.Eq("Deleted", false)}})
}
func (s *Service) ConnectionAccess(ctx context.Context, p core.Principal, id core.ID) (Connection, error) {
	c, err := s.connection(ctx, p.OwnerID, id)
	if err != nil {
		return c, err
	}
	if c.Deleted || !c.Enabled {
		return c, denied()
	}
	if s.options.CheckConnection == nil {
		return c, denied()
	}
	return c, s.options.CheckConnection(ctx, p, id)
}
func (s *Service) view(ctx context.Context, p core.Principal, c Connection, admin bool) (View, error) {
	ts := []Tool{}
	for _, t := range c.Tools {
		if admin || (t.Enabled && s.options.CheckTool != nil && s.options.CheckTool(ctx, p, t.ID) == nil) {
			ts = append(ts, t)
		}
	}
	v := View{ID: c.ID, Name: c.Name, Enabled: c.Enabled, Version: c.Version, Tools: ts, Status: c.Status, CheckedAt: c.CheckedAt, Configured: c.Auth == "none" || c.CredentialID != ""}
	if admin {
		v.Endpoint = c.Endpoint
		v.Auth = c.Auth
		v.Header = c.Header
		v.CallLimit = c.CallLimit
		v.Concurrent = c.Concurrent
		v.TimeoutSeconds = c.TimeoutSeconds
		u, err := quota.CallTotals(ctx, s.store, "connection", "", c.ID, "", time.Time{}, time.Time{})
		if err != nil {
			return v, err
		}
		v.Calls = u.Calls
		v.Active = u.Active
	}
	return v, nil
}
func (s *Service) List(ctx context.Context, p core.Principal) ([]View, error) {
	if err := s.authorize(ctx, p, false); err != nil {
		return nil, err
	}
	admin, err := s.options.IsAdministrator(ctx, p)
	if err != nil {
		return nil, err
	}
	rows, err := s.rows(ctx, p.OwnerID)
	if err != nil {
		return nil, err
	}
	out := []View{}
	for _, c := range rows {
		if !admin {
			if _, err := s.ConnectionAccess(ctx, p, c.ID); err != nil {
				if core.CodeOf(err) == core.Forbidden || core.CodeOf(err) == core.NotFound {
					continue
				}
				return nil, err
			}
		}
		v, err := s.view(ctx, p, c, admin)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func validConfig(q SaveRequest) error {
	u, err := url.Parse(q.Endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || len(q.Endpoint) > 2048 {
		return invalid("an HTTPS endpoint without credentials, query or fragment is required")
	}
	if strings.TrimSpace(q.Name) == "" || len(q.Name) > 120 || q.CallLimit < 1 || q.CallLimit > 9007199254740991 || q.Concurrent < 1 || q.Concurrent > 16 || q.TimeoutSeconds < 1 || q.TimeoutSeconds > 60 {
		return invalid("invalid connection name or limits")
	}
	if len(q.APIKey.Reveal()) > 8192 || strings.ContainsAny(q.APIKey.Reveal(), "\r\n") {
		return invalid("invalid API key")
	}
	switch q.Auth {
	case "none", "bearer":
		if q.Header != "" {
			return invalid("authentication header is only used with header authentication")
		}
	case "header":
		// Restrict custom secrets to API-key headers, never cookies or protocol/routing headers.
		if !regexp.MustCompile(`(?i)^(x-api-key|api-key|x-auth-token)$`).MatchString(q.Header) {
			return invalid("supported headers: X-API-Key, API-Key, X-Auth-Token")
		}
	default:
		return invalid("unsupported authentication type")
	}
	return nil
}

// Resource records let MCP tools use the existing per-assistant grant editor.
func (s *Service) syncResources(ctx context.Context, c Connection, old []Tool) error {
	repo := resource.NewRepository(s.store)
	save := func(id core.ID, kind resource.Kind, name string, enabled bool) error {
		r, err := repo.Get(ctx, c.OwnerID, id)
		fresh := persistence.Missing(err)
		if err != nil && !fresh {
			return err
		}
		previous := r.Version
		r.ID = id
		r.OwnerID = c.OwnerID
		r.Kind = kind
		r.Name = name
		r.Provider = "mcp"
		r.Actions = []string{"execute"}
		r.State = resource.Disconnected
		if enabled && !c.Deleted {
			r.State = resource.Available
		}
		r.Version++
		if fresh {
			return repo.Insert(ctx, r, resource.Binding{ResourceID: id})
		}
		return repo.Save(ctx, r, previous)
	}
	if err := save(c.ID, resource.Connection, c.Name, c.Enabled); err != nil {
		return err
	}
	current := map[core.ID]bool{}
	for _, t := range c.Tools {
		current[t.ID] = true
		if err := save(t.ID, resource.Tool, c.Name+" / "+t.Name, c.Enabled && t.Enabled && t.Problem == ""); err != nil {
			return err
		}
	}
	for _, t := range old {
		if !current[t.ID] {
			if err := save(t.ID, resource.Tool, c.Name+" / "+t.Name, false); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) audit(ctx context.Context, p core.Principal, action string, id core.ID) error {
	return audit.NewRepository(s.store).Append(ctx, audit.Record{ID: newID(), OwnerID: p.OwnerID, SubjectID: p.SubjectID, ActorKind: p.Kind, Action: "mcp." + action, Target: core.Object{Kind: "resource", ID: id}, Outcome: audit.Succeeded, At: time.Now()})
}
func (s *Service) Save(ctx context.Context, p core.Principal, q SaveRequest) (View, error) {
	if err := s.authorize(ctx, p, true); err != nil {
		return View{}, err
	}
	if err := q.Mutation.Validate(q.ID != ""); err != nil {
		return View{}, err
	}
	if err := validConfig(q); err != nil {
		return View{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var c Connection
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		// Idempotency receipts contain only a digest of the write-only credential.
		payload, _ := json.Marshal(struct {
			Request SaveRequest
			KeyHash string
		}{q, hash([]byte(q.APIKey.Reveal()))})
		fingerprint := hash(payload)
		key := persistence.Key(p.OwnerID, core.ID("save/"+q.Mutation.IdempotencyKey))
		var receipt struct {
			Digest string
			ID     core.ID
		}
		if err := tx.Get(ctx, "mcp.mutations", key, &receipt); err == nil {
			if receipt.Digest != fingerprint {
				return conflict()
			}
			var err error
			c, err = s.connection(ctx, p.OwnerID, receipt.ID)
			return err
		} else if !persistence.Missing(err) {
			return err
		}
		owner := catalog.Scope
		c = Connection{ID: core.ID("mcpconn_" + string(newID())), OwnerID: owner, Tools: []Tool{}, Status: "unchecked"}
		if q.ID != "" {
			var err error
			c, err = s.connection(ctx, p.OwnerID, q.ID)
			if err != nil {
				return err
			}
			if c.Deleted || c.Version != q.Mutation.ExpectedVersion {
				return conflict()
			}
			if c.Endpoint != q.Endpoint || c.Auth != q.Auth || c.Header != q.Header {
				return invalid("endpoint and authentication type are immutable; create a new connection")
			}
		} else {
			n, err := tx.Count(ctx, "mcp.connections", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("Deleted", false)}})
			if err != nil {
				return err
			}
			if n >= 32 {
				return invalid("MCP connection limit reached")
			}
		}
		if q.Auth == "none" {
			if !q.APIKey.Empty() {
				return invalid("this connection does not use a credential")
			}
		} else {
			if c.CredentialID == "" && q.APIKey.Empty() {
				return invalid("API key required")
			}
			if !q.APIKey.Empty() {
				if s.options.Credentials == nil {
					return denied()
				}
				var version core.Version
				if c.CredentialID != "" {
					meta, err := s.options.Credentials.Get(ctx, p, c.CredentialID)
					if err != nil {
						return err
					}
					version = meta.Version
				}
				cred, err := s.options.Credentials.Put(ctx, p, credential.PutRequest{ID: c.CredentialID, Provider: "mcp:" + string(c.ID), Label: q.Name, Value: q.APIKey, Verification: q.Verification, Mutation: core.Mutation{IdempotencyKey: q.Mutation.IdempotencyKey, ExpectedVersion: version}})
				if err != nil {
					return err
				}
				c.CredentialID = cred.ID
				// A different credential may expose a different tool catalog. Require discovery again.
				if q.ID != "" {
					q.EnabledTools = nil
					c.Status = "unchecked"
					c.CheckedAt = nil
				}
			}
		}
		for _, id := range q.EnabledTools {
			found := false
			for _, t := range c.Tools {
				if t.ID == id && t.Problem == "" {
					found = true
				}
			}
			if !found {
				return invalid("unknown or unsupported tool")
			}
		}
		for i := range c.Tools {
			c.Tools[i].Enabled = slices.Contains(q.EnabledTools, c.Tools[i].ID) && c.CheckedAt != nil
		}
		c.Name = strings.TrimSpace(q.Name)
		c.Endpoint = q.Endpoint
		c.Auth = q.Auth
		c.Header = q.Header
		c.Enabled = q.Enabled
		c.CallLimit = q.CallLimit
		c.Concurrent = q.Concurrent
		c.TimeoutSeconds = q.TimeoutSeconds
		c.Version++
		if err := s.syncResources(ctx, c, nil); err != nil {
			return err
		}
		if err := tx.Put(ctx, "mcp.connections", persistence.Key(c.OwnerID, c.ID), c); err != nil {
			return err
		}
		if err := tx.Put(ctx, "mcp.mutations", key, struct {
			Digest string
			ID     core.ID
		}{fingerprint, c.ID}); err != nil {
			return err
		}
		return s.audit(ctx, p, "save", c.ID)
	})
	if err != nil {
		return View{}, err
	}
	s.cancelLocked(c.ID)
	return s.view(ctx, p, c, true)
}
func (s *Service) Remove(ctx context.Context, p core.Principal, q Request) error {
	if err := s.authorize(ctx, p, true); err != nil {
		return err
	}
	if err := q.Mutation.Validate(true); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		c, err := s.connection(ctx, p.OwnerID, q.ID)
		if err != nil {
			return err
		}
		if c.Deleted {
			return nil
		}
		if c.Version != q.Mutation.ExpectedVersion {
			return conflict()
		}
		if c.CredentialID != "" {
			meta, err := s.options.Credentials.Get(ctx, p, c.CredentialID)
			if err != nil {
				return err
			}
			if err := s.options.Credentials.Delete(ctx, p, credential.DeleteRequest{ID: c.CredentialID, Verification: q.Verification, Mutation: core.Mutation{IdempotencyKey: q.Mutation.IdempotencyKey, ExpectedVersion: meta.Version}}); err != nil {
				return err
			}
		}
		c.Deleted = true
		c.Enabled = false
		c.CredentialID = ""
		c.Version++
		if err := s.syncResources(ctx, c, nil); err != nil {
			return err
		}
		if err := tx.Put(ctx, "mcp.connections", persistence.Key(c.OwnerID, c.ID), c); err != nil {
			return err
		}
		return s.audit(ctx, p, "remove", c.ID)
	})
	if err == nil {
		s.cancelLocked(q.ID)
	}
	return err
}
func (s *Service) cancelLocked(id core.ID) {
	for _, r := range s.runs {
		if r.Call.Connection.ID == id {
			r.Cancel()
		}
	}
}
