package sandboxruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
	"molr.ai/runtime/internal/shellsession"
	"time"
)

func (s *Remote) Execute(ctx context.Context, e core.Execution, id core.ID, q sm.Request, recheck func(context.Context) error) (result json.RawMessage, resultErr error) {
	defer func() {
		if resultErr != nil {
			s.Remember(e, id, resultErr)
		}
	}()
	client, info, err := s.host.Connect(ctx, e.Principal, id)
	if err != nil {
		return nil, err
	}
	if q.GroupComputeConstraints != nil || hasGroupNetworkQuotas(q.NetworkAuthorizations) {
		if !info.UserGroupQuotas {
			return nil, core.WithReason(core.NewError(core.FailedPrecondition, "upgrade remote sandbox for user group quotas"), "group_quota_backend_unavailable")
		}
	}
	q.Identity = identity(e)
	q.Epoch, err = s.host.Epoch(ctx, e.Principal.OwnerID)
	if err != nil {
		return nil, err
	}
	q.Deadline = time.Now().Add(time.Hour - time.Second)
	if q.Method == "shell_execute" && shellsession.ManagedFor(ctx, e) != nil {
		q.Deadline = time.Time{}
	}
	relevant := map[core.ID]bool{}
	if info.Capabilities.NetworkToggle() && !q.FullAccess {
		for _, n := range q.NetworkAuthorizations {
			if n.ExpiresAt != nil && (q.Deadline.IsZero() || n.ExpiresAt.Before(q.Deadline)) {
				q.Deadline = *n.ExpiresAt
			}
		}
	}

	for _, scope := range q.Scopes {
		relevant[scope.ResourceID] = true
	}
	if q.Compute {
		var shell tools.ShellRequest
		if json.Unmarshal(q.Arguments, &shell) == nil {
			relevant[shell.ComputeID] = true
		}
	}
	grants, grantErr := persistence.Select[policy.Grant](ctx, s.store, "security.grants", persistence.Query{Where: []persistence.Filter{persistence.Eq("RevokedAt", nil), persistence.Eq("OwnerID", e.Principal.OwnerID), persistence.Eq("AssistantID", e.AssistantID)}, Any: [][]persistence.Filter{{persistence.Eq("ExpiresAtKey", "")}, {{Field: "ExpiresAtKey", Op: ">", Value: persistence.TimeKey(time.Now())}}}})
	if grantErr != nil {
		return nil, grantErr
	}
	for _, g := range grants {
		if q.FullAccess && !q.Compute && g.OwnerID == e.Principal.OwnerID && g.AssistantID == e.AssistantID && g.Scope.FullAccess {
			res, err := resource.NewRepository(s.store).Get(ctx, e.Principal.OwnerID, g.ResourceID)
			if err == nil && res.Kind == resource.Compute && (res.SandboxID == id || res.SandboxID == "" && id == "local") {
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
		status, statusErr := persistence.Load[agent.Status](ctx, s.store, "agent.status", persistence.Key(e.Principal.OwnerID, e.AssistantID))
		if statusErr != nil && !persistence.Missing(statusErr) {
			return statusErr
		}
		if shellsession.ManagedFor(ctx, e) == nil && (status.State == agent.Paused || status.State == agent.Stopped) {
			return denied()
		}
		op, opErr := operation.NewRepository(s.store).Get(ctx, e.Principal.OwnerID, e.OperationID)
		if opErr != nil && !persistence.Missing(opErr) {
			return opErr
		}
		if shellsession.ManagedFor(ctx, e) == nil && opErr == nil && (op.State == operation.Canceled || op.State == operation.Failed) {
			return denied()
		}
		if err := s.host.CheckExecution(ctx, e); err != nil {
			return err
		}
		if err := s.host.CheckConnection(ctx, e.Principal, id); err != nil {
			return err
		}
		now, err := s.host.Epoch(ctx, e.Principal.OwnerID)
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
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
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
func denied() error { return core.NewError(core.Forbidden, "object access denied") }
func hasGroupNetworkQuotas(rows []platform.NetworkAuthorization) bool {
	for _, r := range rows {
		if r.GroupConstraints != nil {
			return true
		}
	}
	return false
}
