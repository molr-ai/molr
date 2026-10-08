package auth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	avatarutil "molr.ai/runtime/internal/avatar"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

const AdminGroupID core.ID = "administrators"
const DefaultGroupID core.ID = "users"

// ResourceReference only refers to the central catalog. It contains no provider,
// endpoint, credential, tool definition or sandbox connection configuration.
type ResourceReference struct {
	AllowFullAccess bool   `json:",omitempty"`
	Kind            string // model, tool, sandbox, connection
	ID              core.ID
	Quota           ResourceQuota
}

// Periodic quotas are per account across all its assistants, never a shared
// group balance. Nil/zero optional ceilings mean unlimited, not a fresh balance.
type ResourceQuota struct {
	Budget     *core.Money `json:",omitempty"`
	Calls      int64
	Concurrent int
	Period     string
	Timezone   string
	Compute    *core.ComputeConstraints `json:",omitempty"`
	Network    *core.NetworkConstraints `json:",omitempty"`
	Storage    *core.StorageConstraints `json:",omitempty"`
}
type UserGroup struct {
	ID            core.ID
	Name          string
	Administrator bool
	Resources     []ResourceReference
	Version       core.Version
}
type UserInfo struct {
	Account
	Administrator bool
	GroupName     string
}
type CreateUserRequest struct {
	Username    string
	DisplayName string
	GroupID     core.ID
	Password    core.Secret
	Mutation    core.Mutation
}
type UpdateUserRequest struct {
	ID          core.ID
	DisplayName string
	GroupID     core.ID
	Disabled    bool
	Mutation    core.Mutation
}
type SaveGroupRequest struct {
	Group    UserGroup
	Mutation core.Mutation
}

func ensureGroups(ctx context.Context, tx persistence.Tx) error {
	for _, g := range []UserGroup{{ID: AdminGroupID, Name: "Administrators", Administrator: true, Resources: []ResourceReference{}, Version: 1}, {ID: DefaultGroupID, Name: "Users", Resources: []ResourceReference{}, Version: 1}} {
		var old UserGroup
		if err := tx.Get(ctx, "auth.groups", string(g.ID), &old); persistence.Missing(err) {
			if err := tx.Put(ctx, "auth.groups", string(g.ID), g); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

// Account storage is validated, never upgraded, by the running service.
// Conversion from an older layout is an explicit offline maintenance step.
func (m *Manager) checkAccountStorage(ctx context.Context) error {
	return m.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Scan(ctx, "auth.accounts")
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		incompatible := func() error {
			return core.NewError(core.FailedPrecondition, "account storage format unsupported; run the offline multi-user migration before starting this version")
		}
		var bootstrap struct{ AccountID core.ID }
		if err := tx.Get(ctx, "auth.bootstrap", "owner", &bootstrap); persistence.Missing(err) {
			return incompatible()
		} else if err != nil {
			return err
		}
		found := false
		for _, row := range rows {
			a, err := persistence.Decode[accountRecord](row)
			if err != nil {
				return err
			}
			if a.Account.ID.Validate() != nil || row.Key != string(a.Account.ID) || a.Account.GroupID == "" {
				return incompatible()
			}
			var g UserGroup
			if err := tx.Get(ctx, "auth.groups", string(a.Account.GroupID), &g); persistence.Missing(err) {
				return incompatible()
			} else if err != nil {
				return err
			}
			if g.ID != a.Account.GroupID || g.Administrator != (g.ID == AdminGroupID) {
				return incompatible()
			}
			found = found || a.Account.ID == bootstrap.AccountID
		}
		if !found {
			return incompatible()
		}
		return nil
	})
}

func GroupFor(ctx context.Context, store persistence.Store, owner core.ID) (UserGroup, error) {
	a, err := NewRepository(store).AccountByID(ctx, owner)
	if err != nil {
		return UserGroup{}, err
	}
	if a.Account.Disabled {
		return UserGroup{}, denied()
	}
	return persistence.Load[UserGroup](ctx, store, "auth.groups", string(a.Account.GroupID))
}
func (m *Manager) CurrentUser(ctx context.Context, p core.Principal) (UserInfo, error) {
	if err := m.owner(ctx, p); err != nil {
		return UserInfo{}, err
	}
	a, err := m.repo.AccountByID(ctx, p.OwnerID)
	if err != nil {
		return UserInfo{}, err
	}
	g, err := GroupFor(ctx, m.store, p.OwnerID)
	return UserInfo{Account: a.Account, Administrator: g.Administrator, GroupName: g.Name}, err
}
func (m *Manager) RequireAdministrator(ctx context.Context, p core.Principal) error {
	if err := m.owner(ctx, p); err != nil {
		return err
	}
	g, err := GroupFor(ctx, m.store, p.OwnerID)
	if err != nil {
		return err
	}
	if !g.Administrator {
		return core.NewError(core.Forbidden, "administrator required")
	}
	return m.CheckScope(ctx, p, "admin")
}
func (m *Manager) Users(ctx context.Context, p core.Principal, q core.PageRequest) (core.Page[UserInfo], error) {
	if err := m.RequireAdministrator(ctx, p); err != nil {
		return core.Page[UserInfo]{}, err
	}
	rows, err := persistence.Page[accountRecord](ctx, m.store, "auth.accounts", persistence.Query{}, q, p.OwnerID, nil)
	if err != nil {
		return core.Page[UserInfo]{}, err
	}
	items := []UserInfo{}
	for _, row := range rows.Items {
		g, err := persistence.Load[UserGroup](ctx, m.store, "auth.groups", string(row.Account.GroupID))
		if err != nil {
			return core.Page[UserInfo]{}, err
		}
		row.Account.Version = row.Version
		items = append(items, UserInfo{Account: row.Account, Administrator: g.Administrator, GroupName: g.Name})
	}
	return core.Page[UserInfo]{Items: items, NextCursor: rows.NextCursor}, nil
}
func (m *Manager) CreateUser(ctx context.Context, p core.Principal, r CreateUserRequest) (Account, error) {
	if err := m.RequireAdministrator(ctx, p); err != nil {
		return Account{}, err
	}
	if strings.TrimSpace(r.Username) != r.Username || r.Username == "" || len(r.Username) > 128 || len(r.DisplayName) > 128 {
		return Account{}, core.Invalid("username", "invalid username or display name")
	}
	if err := r.Mutation.Validate(false); err != nil {
		return Account{}, err
	}
	hash, err := hashPassword(r.Password.Reveal())
	if err != nil {
		return Account{}, err
	}
	var out Account
	err = m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.RequireAdministrator(ctx, p); err != nil {
			return err
		}
		if _, err := persistence.Load[UserGroup](ctx, tx.Store(), "auth.groups", string(r.GroupID)); err != nil {
			return err
		}
		key := persistence.Key(p.OwnerID, core.ID("create-user/"+r.Mutation.IdempotencyKey))
		var previous struct {
			ID              core.ID
			Username, Group string
		}
		if err := tx.Get(ctx, "auth.admin-receipts", key, &previous); err == nil {
			if previous.Username != r.Username || previous.Group != string(r.GroupID) {
				return persistence.Conflict("idempotency conflict")
			}
			a, err := m.repo.AccountByID(ctx, previous.ID)
			out = a.Account
			return err
		} else if !persistence.Missing(err) {
			return err
		}
		out = Account{ID: newID(), Username: r.Username, DisplayName: strings.TrimSpace(r.DisplayName), GroupID: r.GroupID, CreatedAt: m.options.Now(), Version: 1}
		if out.DisplayName == "" {
			out.DisplayName = out.Username
		}
		if err := m.repo.InsertAccount(ctx, StoredAccount{Account: out, PasswordHash: core.NewSecret(hash), Version: 1}); err != nil {
			return err
		}
		if err := tx.Put(ctx, "auth.admin-receipts", key, struct {
			ID              core.ID
			Username, Group string
		}{out.ID, r.Username, string(r.GroupID)}); err != nil {
			return err
		}
		return m.audit(ctx, p.OwnerID, p.SubjectID, "admin.user.create", "succeeded", core.Object{Kind: "account", ID: out.ID})
	})
	return out, err
}
func (m *Manager) UpdateUser(ctx context.Context, p core.Principal, r UpdateUserRequest) (Account, error) {
	if err := r.Mutation.Validate(true); err != nil {
		return Account{}, err
	}
	if len(r.DisplayName) > 128 {
		return Account{}, core.Invalid("display_name", "too long")
	}
	var out Account
	err := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.RequireAdministrator(ctx, p); err != nil {
			return err
		}
		a, err := m.repo.AccountByID(ctx, r.ID)
		if err != nil {
			return err
		}
		if a.Version != r.Mutation.ExpectedVersion {
			return persistence.Conflict("account changed")
		}
		g, err := persistence.Load[UserGroup](ctx, tx.Store(), "auth.groups", string(r.GroupID))
		if err != nil {
			return err
		}
		if a.Account.GroupID == AdminGroupID && !a.Account.Disabled && (r.Disabled || !g.Administrator) {
			rows, err := persistence.Select[accountRecord](ctx, tx.Store(), "auth.accounts", persistence.Query{Where: []persistence.Filter{persistence.Eq("Account.GroupID", AdminGroupID), persistence.Eq("Account.Disabled", false)}})
			if err != nil {
				return err
			}
			others := 0
			for _, other := range rows {
				if other.Account.ID != r.ID && other.Account.GroupID == AdminGroupID && !other.Account.Disabled {
					others++
				}
			}
			if others == 0 {
				return core.NewError(core.FailedPrecondition, "last administrator cannot be disabled or demoted")
			}
		}
		changed := a.Account.Disabled != r.Disabled || a.Account.GroupID != r.GroupID
		a.Account.DisplayName, a.Account.GroupID, a.Account.Disabled = strings.TrimSpace(r.DisplayName), r.GroupID, r.Disabled
		if err := m.repo.SaveAccount(ctx, a, a.Version); err != nil {
			return err
		}
		if changed {
			if err := m.repo.RevokeAll(ctx, r.ID, m.options.Now()); err != nil {
				return err
			}
		}
		out = a.Account
		out.Version = a.Version + 1
		return m.audit(ctx, p.OwnerID, p.SubjectID, "admin.user.update", "succeeded", core.Object{Kind: "account", ID: out.ID})
	})
	if err == nil && m.options.AfterAccessChange != nil {
		m.options.AfterAccessChange(ctx)
	}
	return out, err
}
func (m *Manager) Groups(ctx context.Context, p core.Principal) ([]UserGroup, error) {
	if err := m.RequireAdministrator(ctx, p); err != nil {
		return nil, err
	}
	return persistence.Scan[UserGroup](ctx, m.store, "auth.groups")
}
func (m *Manager) SaveGroup(ctx context.Context, p core.Principal, r SaveGroupRequest) (UserGroup, error) {
	if err := r.Mutation.Validate(r.Group.ID != ""); err != nil {
		return UserGroup{}, err
	}
	g := r.Group
	if strings.TrimSpace(g.Name) == "" || len(g.Name) > 128 || g.Administrator || g.ID == AdminGroupID || len(g.Resources) > 1000 {
		return UserGroup{}, core.Invalid("group", "invalid ordinary user group")
	}
	seen := map[string]bool{}
	for _, ref := range g.Resources {
		if (ref.AllowFullAccess && ref.Kind != "sandbox") || ref.ID.Validate() != nil || (ref.Kind != "model" && ref.Kind != "tool" && ref.Kind != "sandbox" && ref.Kind != "connection" && ref.Kind != "plugin") {
			return UserGroup{}, core.Invalid("resource", "catalog reference required")
		}
		key := ref.Kind + ":" + string(ref.ID)
		if seen[key] {
			return UserGroup{}, core.Invalid("resource", "duplicate reference")
		}
		seen[key] = true
		q := ref.Quota
		if ref.Kind == "plugin" && (q.Calls != 0 || q.Concurrent != 0 || q.Budget != nil) {
			return UserGroup{}, core.Invalid("quota", "plugin access has no quota")
		}
		if q.Calls < 0 || q.Concurrent < 0 || (ref.Kind == "sandbox" && q.Calls != 0) {
			return UserGroup{}, core.Invalid("quota", "negative limit")
		}
		if q.Budget != nil {
			if err := q.Budget.Validate(); err != nil {
				return UserGroup{}, err
			}
			if ref.Kind != "model" {
				return UserGroup{}, core.Invalid("quota", "money budget requires a model")
			}
		}
		if q.Calls > 0 || q.Budget != nil {
			if q.Period != "day" && q.Period != "week" && q.Period != "month" && q.Period != "lifetime" {
				return UserGroup{}, core.Invalid("period", "explicit quota period required")
			}
			if _, err := time.LoadLocation(q.Timezone); q.Timezone == "" || err != nil {
				return UserGroup{}, core.Invalid("timezone", "explicit timezone required")
			}
		}
		for _, c := range []*core.ResourceConstraints{{Compute: q.Compute}, {Network: q.Network}, {Storage: q.Storage}} {
			if c.Compute == nil && c.Network == nil && c.Storage == nil {
				continue
			}
			if ref.Kind != "sandbox" {
				return UserGroup{}, core.Invalid("quota", "sandbox constraint requires sandbox reference")
			}
			if err := c.Validate(); err != nil {
				return UserGroup{}, err
			}
		}
	}
	err := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.RequireAdministrator(ctx, p); err != nil {
			return err
		}
		if m.options.ValidateResources != nil {
			if err := m.options.ValidateResources(ctx, p, g.Resources); err != nil {
				return err
			}
		}
		if g.ID == "" {
			key := persistence.Key(p.OwnerID, core.ID("create-group/"+r.Mutation.IdempotencyKey))
			raw, _ := json.Marshal(r.Group)
			fingerprint := sha256.Sum256(raw)
			var receipt struct {
				ID          core.ID
				Fingerprint [32]byte
			}
			if err := tx.Get(ctx, "auth.admin-receipts", key, &receipt); err == nil {
				if receipt.Fingerprint != fingerprint {
					return persistence.Conflict("idempotency conflict")
				}
				g, err = persistence.Load[UserGroup](ctx, tx.Store(), "auth.groups", string(receipt.ID))
				return err
			} else if !persistence.Missing(err) {
				return err
			}
			g.ID = newID()
			if err := tx.Put(ctx, "auth.admin-receipts", key, struct {
				ID          core.ID
				Fingerprint [32]byte
			}{g.ID, fingerprint}); err != nil {
				return err
			}
			g.Version = 1
		} else {
			old, err := persistence.Load[UserGroup](ctx, tx.Store(), "auth.groups", string(g.ID))
			if err != nil {
				return err
			}
			if old.Version != r.Mutation.ExpectedVersion {
				return persistence.Conflict("group changed")
			}
			g.Version = old.Version + 1
		}
		if g.Resources == nil {
			g.Resources = []ResourceReference{}
		}
		if err := tx.Put(ctx, "auth.groups", string(g.ID), g); err != nil {
			return err
		}
		return m.audit(ctx, p.OwnerID, p.SubjectID, "admin.group.save", "succeeded", core.Object{Kind: "user_group", ID: g.ID})
	})
	if err == nil && m.options.AfterAccessChange != nil {
		m.options.AfterAccessChange(ctx)
	}
	return g, err
}
func (m *Manager) DeleteGroup(ctx context.Context, p core.Principal, id core.ID, version core.Version) error {
	return m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.RequireAdministrator(ctx, p); err != nil {
			return err
		}
		if id == AdminGroupID || id == DefaultGroupID {
			return core.NewError(core.Forbidden, "built-in group cannot be deleted")
		}
		g, err := persistence.Load[UserGroup](ctx, tx.Store(), "auth.groups", string(id))
		if err != nil {
			return err
		}
		if g.Version != version {
			return persistence.Conflict("group changed")
		}
		users, err := persistence.Select[accountRecord](ctx, tx.Store(), "auth.accounts", persistence.Query{Where: []persistence.Filter{persistence.Eq("Account.GroupID", id)}, Limit: 1})
		if err != nil {
			return err
		}
		for _, u := range users {
			if u.Account.GroupID == id {
				return core.NewError(core.FailedPrecondition, "move group members first")
			}
		}
		if err := tx.Delete(ctx, "auth.groups", string(id)); err != nil {
			return err
		}
		return m.audit(ctx, p.OwnerID, p.SubjectID, "admin.group.delete", "succeeded", core.Object{Kind: "user_group", ID: id})
	})
}
func (m *Manager) UpdateProfile(ctx context.Context, p core.Principal, name string, avatar *string, version core.Version) (UserInfo, error) {
	if strings.TrimSpace(name) == "" || len(name) > 128 {
		return UserInfo{}, core.Invalid("display_name", "name required")
	}
	err := m.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := m.ownerSession(ctx, p); err != nil {
			return err
		}
		a, err := m.repo.AccountByID(ctx, p.OwnerID)
		if err != nil {
			return err
		}
		if avatar != nil {
			a.Account.Avatar, err = avatarutil.Normalize(*avatar)
			if err != nil {
				return err
			}
		}
		a.Account.DisplayName = strings.TrimSpace(name)
		return m.repo.SaveAccount(ctx, a, version)
	})
	if err != nil {
		return UserInfo{}, err
	}
	return m.CurrentUser(ctx, p)
}
