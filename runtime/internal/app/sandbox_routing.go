package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path"
	"reflect"
	"time"

	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/media"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

func targetSandbox(id core.ID) core.ID {
	if id == "" {
		return "local"
	}
	return id
}

func sandboxOf(r resource.Resource) core.ID {
	if r.SandboxID == "" {
		return "local"
	}
	return r.SandboxID
}
func (s *sandboxRouter) location(ctx context.Context, e core.Execution, t tools.FileTarget) (core.ID, error) {
	if err := (access{s.adapters.store}).execution(ctx, e); err != nil {
		return "", err
	}
	id := targetSandbox(t.SandboxID)
	if t.ResourceID != "" {
		r, err := resource.NewRepository(s.adapters.store).Get(ctx, e.Principal.OwnerID, t.ResourceID)
		if err != nil {
			return "", err
		}
		if r.Kind != resource.Storage {
			return "", denied()
		}
		if id != sandboxOf(r) {
			return "", core.Invalid("sandbox_id", "explicit sandbox_id must match the resource node")
		}
	} else if id != "local" && !t.FullAccess {
		return "", core.Invalid("resource_id", "remote computers have no assistant workspace; select an authorized directory")
	}
	if _, err := groupReference(ctx, s.adapters.store, e.Principal.OwnerID, "sandbox", id); err != nil {
		return "", err
	}
	if id == "local" {
		if !s.adapters.storageConfig.Enabled {
			return "", core.WithReason(core.NewError(core.Unavailable, "local sandbox disabled"), "sandbox_disabled")
		}
	} else {
		if _, err := s.connection(ctx, e.Principal, id); err != nil {
			return "", err
		}
	}
	return id, nil
}
func (s *sandboxRouter) scope(ctx context.Context, e core.Execution, id core.ID, t tools.FileTarget, actions ...policy.Action) (sm.Scope, error) {
	if t.ResourceID == "" {
		return sm.Scope{}, nil
	}
	name, err := resourcePath(t.Path)
	if err != nil {
		return sm.Scope{}, err
	}
	r, b, err := s.adapters.binding(ctx, e.Principal, t.ResourceID)
	if err != nil {
		return sm.Scope{}, err
	}
	if r.Kind != resource.Storage || sandboxOf(r) != id {
		return sm.Scope{}, denied()
	}
	scope := sm.Scope{ResourceID: r.ID, DirectoryID: b.SandboxResource, Path: name}
	if t.FullAccess {
		if err := s.requireFullAccess(ctx, e, id); err != nil {
			return scope, err
		}
		scope.Read = true
		scope.Write = true
		return scope, nil
	}

	for _, a := range actions {
		if err = s.adapters.policy.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: r.ID}, ResourceID: r.ID, Action: a, Destination: name}); err != nil {
			return scope, err
		}
		if a == policy.Read {
			scope.Read = true
		}
		if a == policy.Write {
			scope.Write = true
		}
	}
	return scope, nil
}
func identity(e core.Execution) sm.Identity {
	return sm.Identity{OwnerID: e.Principal.OwnerID, AssistantID: e.AssistantID, ExecutionID: e.Principal.ExecutionID, OperationID: e.OperationID}
}
func randomOperation() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID("sbread_" + hex.EncodeToString(b[:]))
}
func (s *sandboxRouter) remote(ctx context.Context, e core.Execution, id core.ID, q sm.Request, recheck func(context.Context) error) (result json.RawMessage, resultErr error) {
	defer func() {
		if resultErr != nil {
			s.rememberRemoteWait(e, id, resultErr)
		}
	}()
	c, err := s.connection(ctx, e.Principal, id)
	if err != nil {
		return nil, err
	}
	client, err := s.client(ctx, e.Principal, c)
	if err != nil {
		return nil, err
	}
	if q.GroupComputeConstraints != nil || hasGroupNetworkQuotas(q.NetworkAuthorizations) {
		if !c.View.Info.UserGroupQuotas {
			return nil, core.WithReason(core.NewError(core.FailedPrecondition, "upgrade remote sandbox for user group quotas"), "group_quota_backend_unavailable")
		}
	}
	q.Identity = identity(e)

	if err != nil {
		return nil, err
	}
	q.Epoch, err = s.epoch(ctx, e.Principal.OwnerID)
	if err != nil {
		return nil, err
	}
	q.Deadline = time.Now().Add(time.Hour - time.Second)
	if q.Method == "shell_execute" && managedShellFor(ctx, e) != nil {
		q.Deadline = time.Time{}
	}
	relevant := map[core.ID]bool{}
	for _, scope := range q.Scopes {
		relevant[scope.ResourceID] = true
	}
	if q.Compute {
		var shell tools.ShellRequest
		if json.Unmarshal(q.Arguments, &shell) == nil {
			relevant[shell.ComputeID] = true
		}
	}
	grants, grantErr := persistence.Select[policy.Grant](ctx, s.adapters.store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("RevokedAt", nil), persistence.Eq("OwnerID", e.Principal.OwnerID), persistence.Eq("AssistantID", e.AssistantID)}, Any: [][]persistence.Filter{{persistence.Eq("ExpiresAtKey", "")}, {{Field: "ExpiresAtKey", Op: ">", Value: persistence.TimeKey(time.Now())}}}})
	if grantErr != nil {
		return nil, grantErr
	}
	for _, g := range grants {
		if q.FullAccess && !q.Compute && g.OwnerID == e.Principal.OwnerID && g.AssistantID == e.AssistantID && g.Scope.FullAccess {
			res, err := resource.NewRepository(s.adapters.store).Get(ctx, e.Principal.OwnerID, g.ResourceID)
			if err == nil && res.Kind == resource.Compute && sandboxOf(res) == id {
				relevant[g.ResourceID] = true
			}
		}
		if g.OwnerID == e.Principal.OwnerID && g.AssistantID == e.AssistantID && relevant[g.ResourceID] && g.Scope.FullAccess == q.FullAccess && g.RevokedAt == nil && g.ExpiresAt != nil && g.ExpiresAt.After(time.Now()) && (q.Deadline.IsZero() || g.ExpiresAt.Before(q.Deadline)) {
			q.Deadline = *g.ExpiresAt
		}
	}

	epoch := q.Epoch
	switch q.Method {
	case "snapshot", "download":
		q.Identity.OperationID = randomOperation()
	}
	check := func(ctx context.Context) error {
		status, statusErr := persistence.Load[agent.Status](ctx, s.adapters.store, "agent.status", persistence.Key(e.Principal.OwnerID, e.AssistantID))
		if statusErr != nil && !persistence.Missing(statusErr) {
			return statusErr
		}
		if managedShellFor(ctx, e) == nil && (status.State == agent.Paused || status.State == agent.Stopped) {
			return denied()
		}
		op, opErr := operation.NewRepository(s.adapters.store).Get(ctx, e.Principal.OwnerID, e.OperationID)
		if opErr != nil && !persistence.Missing(opErr) {
			return opErr
		}
		if managedShellFor(ctx, e) == nil && opErr == nil && (op.State == operation.Canceled || op.State == operation.Failed) {
			return denied()
		}
		if err := (access{s.adapters.store}).execution(ctx, e); err != nil {
			return err
		}
		if _, err := s.connection(ctx, e.Principal, id); err != nil {
			return err
		}
		now, err := s.epoch(ctx, e.Principal.OwnerID)
		if err != nil {
			return err
		}
		if now != epoch {
			return denied()
		}
		return recheck(ctx)
	}

	// Freeze routing and execution identity before effects. A recovered Runtime
	// attempt must query the original operation rather than overwrite its identity.
	if q.Method == "file_write" || q.Method == "file_edit" || q.Method == "file_move" || q.Method == "file_delete" || q.Method == "shell_execute" || q.Method == "upload" {
		type target struct {
			SandboxID core.ID
			Identity  sm.Identity
		}
		var prior target
		existed := false
		err = s.adapters.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			key := persistence.Key(e.Principal.OwnerID, q.Identity.OperationID)
			err := tx.Get(ctx, "sandbox.operation_targets", key, &prior)
			if err == nil {
				if prior.SandboxID != id || prior.Identity.OwnerID != e.Principal.OwnerID || prior.Identity.AssistantID != e.AssistantID {
					return denied()
				}
				existed = true
				return nil
			}
			if !persistence.Missing(err) {
				return err
			}
			return tx.Put(ctx, "sandbox.operation_targets", key, target{id, q.Identity})
		})
		if err != nil {
			return nil, err
		}
		if existed {
			return client.Await(ctx, prior.Identity, check)
		}
	}

	return client.Execute(ctx, q, check)
}
func (s *sandboxRouter) file(ctx context.Context, e core.Execution, method string, t tools.FileTarget, args any, modelID core.ID) (json.RawMessage, bool, error) {
	action := policy.Read
	if method == "file_write" || method == "file_edit" || method == "file_move" || method == "file_delete" || method == "upload" {
		action = policy.Write
	}
	if err := checkFilePolicy(ctx, e, t, action); err != nil {
		return nil, true, err
	}
	id, err := s.location(ctx, e, t)
	if err != nil {
		return nil, true, err
	}
	if t.FullAccess {
		if err := s.requireFullAccess(ctx, e, id); err != nil {
			return nil, true, err
		}
	}
	if id == "local" {
		return nil, false, nil
	}
	actions := []policy.Action{policy.Read}
	if method == "file_write" || method == "file_move" || method == "file_delete" || method == "upload" {
		actions = []policy.Action{policy.Write}
	}
	if method == "file_edit" {
		actions = append(actions, policy.Write)
	}
	var scope sm.Scope
	if !t.FullAccess || t.ResourceID != "" {
		scope, err = s.scope(ctx, e, id, t, actions...)
	}
	if err != nil {
		return nil, true, err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, true, err
	}
	q := sm.Request{FullAccess: t.FullAccess, Method: method, Arguments: raw}
	if read, ok := args.(tools.FileReadRequest); ok {
		q.TextRange = read.TextRange
	}
	if t.ResourceID != "" {
		q.Scopes = []sm.Scope{scope}
	}
	var moveDestination *tools.FileTarget
	if move, ok := args.(tools.FileMoveRequest); ok {
		dest := t
		dest.Path = move.ToPath
		moveDestination = &dest
		if !t.FullAccess || t.ResourceID != "" {
			destinationScope, err := s.scope(ctx, e, id, dest, policy.Write)
			if err != nil {
				return nil, true, err
			}
			q.Scopes = append(q.Scopes, destinationScope)
		}
	}
	parentTargets := []tools.FileTarget{}
	var createTarget *tools.FileTarget
	if write, ok := args.(tools.FileWriteRequest); ok && write.CreateParents {
		v := t
		createTarget = &v
	}
	if move, ok := args.(tools.FileMoveRequest); ok && move.CreateParents {
		v := t
		v.Path = move.ToPath
		createTarget = &v
	}
	if createTarget != nil && !t.FullAccess {
		for name := path.Dir(createTarget.Path); name != "."; name = path.Dir(name) {
			v := *createTarget
			v.Path = name
			parentScope, err := s.scope(ctx, e, id, v, policy.Write)
			if err == nil {
				parentTargets = append(parentTargets, v)
				q.Scopes = append(q.Scopes, parentScope)
			} else if core.CodeOf(err) != core.Forbidden && core.CodeOf(err) != core.NotFound {
				return nil, true, err
			}
			if name == "/" {
				break
			}
			if len(q.Scopes) > 32 {
				return nil, true, core.Invalid("path", "too many parent scopes")
			}
		}
	}
	if modelID != "" {
		provider, err := s.adapters.provider(ctx, e.Principal, modelID)
		if err != nil {
			return nil, true, err
		}
		if err = s.adapters.policy.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: provider.Model.ResourceID}, ResourceID: provider.Model.ResourceID, ModelID: modelID, Action: policy.Execute, Destination: provider.Endpoint}); err != nil {
			return nil, true, err
		}
		q.Images = provider.Model.SupportsImages
		q.Audio = provider.Model.SupportsAudio
		q.Protocol = string(provider.Protocol)
		q.AudioPricing = provider.Pricing != nil && provider.Pricing.AudioInputMicrosPerMillion != nil
	}
	out, err := s.remote(ctx, e, id, q, func(ctx context.Context) error {
		actual, err := s.location(ctx, e, t)
		if err != nil {
			return err
		}
		if actual != id {
			return denied()
		}
		if t.FullAccess {
			return s.requireFullAccess(ctx, e, id)
		}
		if _, err = s.scope(ctx, e, id, t, actions...); err != nil {
			return err
		}
		for _, parent := range parentTargets {
			if _, err = s.scope(ctx, e, id, parent, policy.Write); err != nil {
				return err
			}
		}
		if moveDestination != nil {
			_, err = s.scope(ctx, e, id, *moveDestination, policy.Write)
		}
		return err
	})
	if err == nil && method == "file_read" {
		var result map[string]json.RawMessage
		if json.Unmarshal(out, &result) == nil {
			result["SandboxID"], _ = json.Marshal(id)
			if raw := result["Media"]; len(raw) > 0 {
				var parts []media.Part
				if json.Unmarshal(raw, &parts) == nil {
					for i := range parts {
						parts[i].SandboxID = id
					}
					result["Media"], _ = json.Marshal(parts)
					out, _ = json.Marshal(result)
				}
			}

			out, _ = json.Marshal(result)
		}
	}
	return out, true, err
}
func (s *sandboxRouter) shellExecute(ctx context.Context, e core.Execution, req tools.ShellRequest) (result json.RawMessage, resultErr error) {
	defer func() {
		if targetSandbox(req.SandboxID) != "local" && resultErr != nil {
			s.rememberRemoteWait(e, targetSandbox(req.SandboxID), resultErr)
		}
	}()
	r, _, err := s.adapters.binding(ctx, e.Principal, req.ComputeID)
	if err != nil {
		return nil, err
	}
	if r.Kind != resource.Compute {
		return nil, denied()
	}
	id := targetSandbox(req.SandboxID)
	if sandboxOf(r) != id {
		return nil, core.Invalid("sandbox_id", "explicit sandbox_id must match the compute resource node")
	}
	info, err := s.info(ctx, e.Principal, id)
	if err != nil {
		return nil, err
	}
	direct := req.FullAccess
	if direct && (!info.Enabled || !info.Capabilities.FullAccess) {
		return nil, core.WithReason(core.NewError(core.Forbidden, "Full Access unavailable"), "full_access_unavailable")
	}
	if direct {
		if req.NetworkMode == "none" {
			return nil, core.Invalid("network_mode", "Full Access uses the environment network; network isolation is unavailable")
		}
		req.Files = nil
		req.NetworkIDs = []core.ID{}
	}
	files := req.Files[:0]
	for _, f := range req.Files {
		if id != "local" || !isWorkspaceResource(ctx, s.adapters.store, e.Principal.OwnerID, f.ResourceID, id) {
			files = append(files, f)
		}
	}
	req.Files = files
	if req.NetworkMode == "none" {
		req.NetworkIDs = nil
	} else if req.NetworkIDs == nil {
		req.NetworkIDs, err = automaticNetworks(ctx, s.adapters.store, e, id)
		if err != nil {
			return nil, err
		}
		info, err := s.info(ctx, e.Principal, id)
		if err != nil {
			return nil, err
		}
		available := map[string]bool{}
		for _, n := range info.Networks {
			available[n.ID] = true
		}
		selected := req.NetworkIDs[:0]
		for _, networkID := range req.NetworkIDs {
			binding, err := resource.NewRepository(s.adapters.store).Binding(ctx, e.Principal.OwnerID, networkID)
			if err != nil {
				return nil, err
			}
			source := binding.SandboxResource
			if id == "local" {
				source = binding.SandboxTarget
			}
			if available[source] {
				selected = append(selected, networkID)
			}
		}
		req.NetworkIDs = selected
	}
	if scope := managedShellFor(ctx, e); scope != nil {
		if err := scope.routed(id); err != nil {
			return nil, err
		}
	}
	if id == "local" {
		if s.shell == nil {
			return nil, core.NewError(core.Unavailable, "local sandbox disabled")
		}
		for _, f := range req.Files {
			v, err := resource.NewRepository(s.adapters.store).Get(ctx, e.Principal.OwnerID, f.ResourceID)
			if err != nil {
				return nil, err
			}
			if sandboxOf(v) != id {
				return nil, core.Invalid("files", "cross-sandbox mounts require explicit file transfer")
			}
		}
		for _, n := range req.NetworkIDs {
			v, err := resource.NewRepository(s.adapters.store).Get(ctx, e.Principal.OwnerID, n)
			if err != nil {
				return nil, err
			}
			if sandboxOf(v) != id {
				return nil, denied()
			}
		}
		return s.shell.execute(ctx, e, req)
	}
	raw, _ := json.Marshal(req)
	q := sm.Request{FullAccess: req.FullAccess, Method: "shell_execute", Arguments: raw, Compute: true}
	if !direct {
		cg, err := executionGrant(ctx, s.adapters.store, e, req.ComputeID)
		if err != nil {
			return nil, err
		}
		if limits := cg.EffectiveConstraints(time.Now()); limits != nil {
			q.ComputeConstraints = limits.Compute
		}

		ref, err := groupReference(ctx, s.adapters.store, e.Principal.OwnerID, "sandbox", id)
		if err != nil {
			return nil, err
		}
		if ref != nil {
			q.GroupComputeConstraints = ref.Quota.Compute
		}
	}
	initialized := false
	check := func(ctx context.Context) error {
		currentRef, err := groupReference(ctx, s.adapters.store, e.Principal.OwnerID, "sandbox", id)
		if err != nil {
			return err
		}
		var currentCompute *policy.ComputeConstraints
		if currentRef != nil {
			currentCompute = currentRef.Quota.Compute
		}
		if !direct && !reflect.DeepEqual(currentCompute, q.GroupComputeConstraints) {
			return denied()
		}
		if err := s.adapters.policy.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: req.ComputeID}, ResourceID: req.ComputeID, Action: policy.Execute, FullAccess: req.FullAccess, Destination: "/"}); err != nil {
			return err
		}
		if direct {
			return nil
		}
		cg, err := executionGrant(ctx, s.adapters.store, e, req.ComputeID)
		if err != nil {
			return err
		}
		current := cg.EffectiveConstraints(time.Now())
		if current == nil || !reflect.DeepEqual(current.Compute, q.ComputeConstraints) {
			return denied()
		}
		if currentRef != nil {
			if err := core.CheckGPUDevices(current.Compute.GPUDevices(), currentRef.Quota.Compute.GPUDevices()); err != nil {
				return err
			}
		}
		if initialized {
			rows := append([]platform.NetworkAuthorization(nil), q.NetworkAuthorizations...)
			for i, n := range rows {
				g, err := persistence.Load[policy.Grant](ctx, s.adapters.store, "security.grants", persistence.Key(e.Principal.OwnerID, n.GrantID))
				if err != nil {
					return err
				}
				rows[i], err = groupNetworkAuthorization(ctx, s.adapters.store, e.Principal.OwnerID, id, rows[i])
				if err != nil {
					return err
				}
				rows[i].ExpiresAt = g.ExpiresAt
				res, err := resource.NewRepository(s.adapters.store).Get(ctx, e.Principal.OwnerID, g.ResourceID)
				if err != nil {
					return err
				}
				if g.RevokedAt != nil || res.State != resource.Available {
					past := time.Unix(0, 0)
					rows[i].ExpiresAt = &past
				}
				if limits := g.EffectiveConstraints(time.Now()); limits != nil {
					rows[i].Constraints = limits.Network
				}
			}
			conn, err := s.connection(ctx, e.Principal, id)
			if err != nil {
				return err
			}
			client, err := s.client(ctx, e.Principal, conn)
			if err != nil {
				return err
			}
			err = client.UpdateQuotas(ctx, sm.Request{Identity: identity(e), NetworkAuthorizations: rows, Arguments: json.RawMessage(`{}`)})
			if core.CodeOf(err) == core.NotFound {
				return nil
			}
			return err
		}
		q.Scopes = nil
		q.Networks = nil
		q.NetworkAuthorizations = nil
		for _, f := range req.Files {
			actions := []policy.Action{policy.Read}
			if f.Write {
				actions = append(actions, policy.Write)
			}
			scope, err := s.scope(ctx, e, id, tools.FileTarget{ResourceID: f.ResourceID, Path: f.Path}, actions...)
			if err != nil {
				return err
			}
			q.Scopes = append(q.Scopes, scope)
		}
		for _, n := range req.NetworkIDs {
			r, b, err := s.adapters.binding(ctx, e.Principal, n)
			if err != nil {
				return err
			}
			if r.Kind != resource.Network || sandboxOf(r) != id {
				return denied()
			}
			if err = s.adapters.policy.Require(ctx, e.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: n}, ResourceID: n, Action: policy.Execute, Destination: "/"}); err != nil {
				return err
			}
			q.Networks = append(q.Networks, b.SandboxResource)
			ng, err := executionGrant(ctx, s.adapters.store, e, n)
			if err != nil {
				return err
			}
			na := platform.NetworkAuthorization{Source: b.SandboxResource, GrantID: ng.ID, ExpiresAt: ng.ExpiresAt}
			if limits := ng.EffectiveConstraints(time.Now()); limits != nil {
				na.Constraints = limits.Network
			}
			na, err = groupNetworkAuthorization(ctx, s.adapters.store, e.Principal.OwnerID, id, na)
			if err != nil {
				return err
			}
			q.NetworkAuthorizations = append(q.NetworkAuthorizations, na)
		}
		return nil
	}
	if err = check(ctx); err != nil {
		return nil, err
	}
	initialized = true
	if scope := managedShellFor(ctx, e); scope != nil {
		connection, err := s.connection(ctx, e.Principal, id)
		if err != nil {
			return nil, err
		}
		client, err := s.client(ctx, e.Principal, connection)
		if err != nil {
			return nil, err
		}
		info, err := client.Info(ctx)
		if err != nil {
			return nil, err
		}
		if (q.ComputeConstraints != nil || hasNetworkQuotas(q.NetworkAuthorizations)) && !info.ResourceQuotas {
			return nil, core.NewError(core.FailedPrecondition, "upgrade remote sandbox for resource quotas")
		}
		if len(req.ForwardPorts) > 0 && !info.PortForwarding {
			return nil, core.NewError(core.FailedPrecondition, "upgrade remote sandbox for TCP port forwarding")
		}
		if !info.ShellSessions {
			return nil, core.NewError(core.FailedPrecondition, "upgrade remote sandbox for shell sessions")
		}
		// Open returns only after the remote shell's output archive exists.
		// Observation uses a read-only request, never resubmits the command.
		observe, stopObserve := context.WithCancel(ctx)
		observed := make(chan struct{})
		defer func() { stopObserve(); <-observed }()
		control := func(call context.Context, request tools.ShellTaskRequest) (json.RawMessage, error) {
			return s.remoteShellControl(call, e, id, request)
		}
		go func() {
			defer close(observed)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				if raw, err := control(observe, tools.ShellTaskRequest{Action: "read", TaskID: e.OperationID}); err == nil {
					var status struct{ Started bool }
					if json.Unmarshal(raw, &status) == nil && status.Started {
						_ = scope.started("", "", q.ComputeConstraints.GPUDevices(), control)
						_ = s.mirrorShellLogs(observe, e, id)
					}
				}
				select {
				case <-observe.Done():
					return
				case <-ticker.C:
				}
			}
		}()

	}
	out, err := s.remote(ctx, e, id, q, check)
	if err != nil {
		return nil, err
	}
	return s.archiveRemoteShellResult(ctx, e, id, out), nil
}

func (s *sandboxRouter) available(ctx context.Context, p core.Principal) (bool, error) {
	if s.shell != nil {
		ok, err := s.shell.available(ctx, p)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	rows, err := persistence.SelectCatalog[resource.Resource](ctx, s.adapters.store, "business.resources", p.OwnerID, persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", resource.Compute), persistence.Eq("State", resource.Available)}})
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if !catalogOwned(ctx, s.adapters.store, r.OwnerID, p.OwnerID) || r.Kind != resource.Compute || r.State != resource.Available || sandboxOf(r) == "local" {
			continue
		}
		c, err := s.connection(ctx, p, r.SandboxID)
		if err != nil {
			continue
		}
		client, err := s.client(ctx, p, c)
		if err != nil {
			continue
		}
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		info, err := client.Info(probe)
		cancel()
		if err == nil && info.Enabled && info.ShellSessions && computeAccess(ctx, s.adapters.policy, p, r.ID, info.Capabilities) {
			return true, nil
		}
	}
	return false, nil
}
func (s *sandboxRouter) cancel(ctx context.Context, e core.Execution, id core.ID) error {
	rows, err := persistence.SelectCatalog[sandboxConnection](ctx, s.adapters.store, "sandbox.connections", e.Principal.OwnerID, persistence.Query{})
	if err != nil {
		return err
	}
	for _, c := range rows {
		if !catalogOwned(ctx, s.adapters.store, c.OwnerID, e.Principal.OwnerID) || c.View.State == "disconnected" {
			continue
		}
		client, err := s.client(ctx, e.Principal, c)
		if err == nil {
			_ = client.Cancel(ctx, identity(e))
		}
	}
	if s.shell != nil {
		return s.shell.cancel(ctx, e, id)
	}
	return nil
}
func (s *sandboxRouter) reconcile(ctx context.Context, op operation.Operation) (operation.Completion, error) {
	target, err := persistence.Load[struct {
		SandboxID core.ID
		Identity  sm.Identity
	}](ctx, s.adapters.store, "sandbox.operation_targets", persistence.Key(op.OwnerID, op.ID))
	if err != nil {
		return operation.Completion{}, err
	}
	// The owner-facing operation service already authorizes reconciliation. Use
	// the original trusted assistant identity solely for the bound transport.
	p := target.Identity.Execution().Principal
	c, err := s.connection(ctx, p, target.SandboxID)
	if err != nil {
		return operation.Completion{}, err
	}
	client, err := s.client(ctx, p, c)
	if err != nil {
		return operation.Completion{}, err
	}
	reply, err := client.Get(ctx, target.Identity)
	if err != nil {
		return operation.Completion{}, err
	}
	if reply.State != "succeeded" {
		return operation.Completion{}, core.NewError(core.FailedPrecondition, "remote outcome unresolved")
	}
	if op.Action == "shell_execute" {
		reply.Result = s.archiveRemoteShellResult(ctx, target.Identity.Execution(), target.SandboxID, reply.Result)
	}
	return operation.Completion{ID: op.ID, ExpectedVersion: op.Version, State: operation.Succeeded, SafeResult: reply.Result}, nil
}
func (s *sandboxRouter) media(ctx context.Context, e core.Execution, p media.Part) ([]byte, bool, error) {
	if p.SandboxID == "" || p.SandboxID == "local" {
		return nil, false, nil
	}
	target := tools.FileTarget{FullAccess: p.FullAccess, SandboxID: p.SandboxID, ResourceID: p.ResourceID, Path: p.Path}
	out, remote, err := s.file(ctx, e, "snapshot", target, p, "")
	if err != nil {
		return nil, remote, err
	}
	var blob sandboxBlob
	err = json.Unmarshal(out, &blob)
	if err == nil && digestBytes(blob.Data) != p.Snapshot {
		err = denied()
	}
	return blob.Data, remote, err
}

type remoteSandboxGrants struct {
	policy.Service
	router *sandboxRouter
}

func (s remoteSandboxGrants) Revoke(ctx context.Context, p core.Principal, r policy.RevokeRequest) error {
	if err := s.Service.Revoke(ctx, p, r); err != nil {
		return err
	}
	g, err := persistence.Load[policy.Grant](ctx, s.router.adapters.store, "security.grants", persistence.Key(p.OwnerID, r.GrantID))
	if err == nil && g.Scope.FullAccess {
		return nil
	}
	if err == nil {
		res, err := resource.NewRepository(s.router.adapters.store).Get(ctx, p.OwnerID, g.ResourceID)
		if err == nil && res.Kind == resource.Network {
			return nil
		}
	}
	return s.router.invalidate(ctx, p)
}
func (s remoteSandboxGrants) UpdateGrant(ctx context.Context, p core.Principal, r policy.UpdateGrantRequest) (policy.Grant, error) {
	g, err := s.Service.UpdateGrant(ctx, p, r)
	if err != nil {
		return g, err
	}
	if batch, _ := ctx.Value(permissionBatchKey{}).(bool); batch {
		return g, nil
	}
	if g.Scope.FullAccess {
		return g, nil
	}
	res, loadErr := resource.NewRepository(s.router.adapters.store).Get(ctx, p.OwnerID, g.ResourceID)
	if loadErr == nil && res.Kind == resource.Network {
		return g, nil
	}
	return g, s.router.invalidate(ctx, p)
}

type remoteSandboxResources struct {
	resource.Service
	router *sandboxRouter
}

func (s remoteSandboxResources) Disconnect(ctx context.Context, p core.Principal, r resource.DisconnectRequest) error {
	if err := s.Service.Disconnect(ctx, p, r); err != nil {
		return err
	}
	return s.router.invalidate(ctx, p)
}

func (s remoteSandboxResources) Check(ctx context.Context, p core.Principal, id core.ID) (resource.Status, error) {
	r, err := s.Service.Get(ctx, p, id)
	if err != nil {
		return resource.Status{}, err
	}
	if sandboxOf(r) == "local" || r.Kind == resource.Model || r.Kind == resource.Tool {
		return s.Service.Check(ctx, p, id)
	}
	info, err := s.router.info(ctx, p, sandboxOf(r))
	status := resource.Status{State: resource.Available, ReasonCode: "available", CheckedAt: time.Now()}
	if err != nil || !info.Enabled {
		status.State = resource.Offline
		status.ReasonCode = "sandbox_offline"
	} else if r.Kind == resource.Compute && !info.Capabilities.Shell && !info.Capabilities.FullAccess {
		status.State = resource.Offline
		status.ReasonCode = info.Capabilities.Reason
	} else if r.Kind == resource.Storage && !info.Storage {
		status.State = resource.Offline
		status.ReasonCode = "storage_unavailable"
	}
	return status, nil
}
