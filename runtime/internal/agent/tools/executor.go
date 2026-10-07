package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/media"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

type Options struct {
	CheckGroupQuota         func(context.Context, Call) error
	ProfileUpdate           func(context.Context, core.Execution, ProfileUpdateRequest) (json.RawMessage, error)
	ExtensionDefinitions    func(context.Context, core.Principal, bool) ([]Definition, error)
	ExtensionValidate       func(context.Context, Call) error
	ExtensionPrepare        func(context.Context, Call) (core.ID, error)
	ExtensionExecute        func(context.Context, Call) (json.RawMessage, error)
	Plan                    func(context.Context, core.Execution, string, json.RawMessage) (json.RawMessage, error)
	ConversationAttachments ConversationAttachments

	PermissionRequest func(context.Context, core.Execution, policy.PermissionRequestInput) (json.RawMessage, error)
	PermissionList    func(context.Context, core.Principal) (json.RawMessage, error)
	ShellTask         func(context.Context, core.Execution, ShellTaskRequest) (json.RawMessage, error)
	ShellExecute      func(context.Context, core.Execution, ShellRequest) (json.RawMessage, error)
	ShellAvailable    func(context.Context, core.Principal) (bool, error)
	Authorizer        policy.Authorizer
	Operations        operation.Service
	Conversations     conversation.Service
	FileSearch        func(context.Context, core.Execution, FileSearchRequest) (json.RawMessage, error)
	FileTransfer      func(context.Context, core.Execution, FileTransferRequest) (json.RawMessage, error)
	FileRead          func(context.Context, core.Execution, FileReadRequest) (json.RawMessage, error)
	FileList          func(context.Context, core.Execution, FileListRequest) (json.RawMessage, error)
	FileWrite         func(context.Context, core.Execution, FileWriteRequest) (json.RawMessage, error)
	FileEdit          func(context.Context, core.Execution, FileEditRequest) (json.RawMessage, error)
	Cancel            func(context.Context, core.Execution, core.ID) error
}
type executor struct {
	store   persistence.Store
	options Options
}

func NewExecutor(store persistence.Store, options Options) Executor { return &executor{store, options} }

type cardArguments struct {
	Kind     conversation.CardKind `json:"kind"`
	TargetID core.ID               `json:"target_id"`
	Title    string                `json:"title,omitempty"`
	Summary  string                `json:"summary,omitempty"`
}
type arguments struct {
	profile      *ProfileUpdateRequest
	fileSearch   *FileSearchRequest
	fileTransfer *FileTransferRequest
	fileRead     *FileReadRequest
	fileList     *FileListRequest
	fileWrite    *FileWriteRequest
	fileEdit     *FileEditRequest
	shellTask    *ShellTaskRequest
	shell        *ShellRequest
	ResourceID   core.ID
	Path         string
}

func schema(properties string, required string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":[` + required + `],"additionalProperties":false}`)
}
func (e *executor) definitions() []Definition {
	definitions := []Definition{}
	if e.options.ProfileUpdate != nil {
		definitions = append(definitions, profileDefinition())
	}
	if e.options.PermissionList != nil {
		definitions = append(definitions, Definition{Name: "permission_list", Description: "List granted permissions and requestable permission choices in separate groups. Requestable.FullAccessAvailable advertises an optional Full Access compute grant; Resources.FullAccess and Sandboxes.FullAccessComputeIDs identify authorized unrestricted calls. Use full_access:true explicitly on shell/file calls only when authorized. Ordinary calls continue using isolation and their separate grants and quotas. Resources[].Grants contains current user grants with exact actions, scopes and expiration; Requestable contains safe resource/model IDs, names, supported actions and maximum resource-relative scope for filling permission_request, NOT authorization to use them. Network resources in both groups include Network.Target (Protocols, Addresses, Ports, ExcludeAddresses, ExcludePorts), system Restrictions, LoopbackAccess and resource Constraints, shared Usage and Requestable.AuthorizationCeilings from the actual sandbox. Network.Known=false means scope could not be confirmed; never infer access from a resource name or an offline snapshot. All ports means 1-65535 for the listed protocols. The sandbox network gateway operates at layer 4 (the transport layer) and supports only TCP/UDP. Protocols outside this scope, such as ICMP, are not forwarded. RequestableTruncated indicates an incomplete choice list. BuiltInStorage is separate from user grants. Resources[].Grants are active user grants; an unavailable operation does not mean its grant is missing. Use permission_request only for missing permissions, not offline resources; request the least actions, scope and time needed. Source=user_grant describes user-authorized resources; BuiltInStorage describes the only assistant workspace, on the local computer. Omitted sandbox_id always means local; remote calls require explicit sandbox_id matching every resource. Remote computers have no assistant workspace. A sandbox being connected does not grant compute, external storage or network access. A resource Path=/ means that resource's root, never the host root. Use this authoritative snapshot when answering permission questions; filesystem mode bits do not establish authorization. Operations are rechecked at use time. No host paths or credentials are returned.", InputSchema: schema("", ""), RequiredActions: []string{"read"}})
	}
	if e.options.PermissionRequest != nil {
		definitions = append(definitions, permissionRequestDefinition())
	}
	if e.options.ShellExecute != nil {
		definitions = append(definitions, Definition{Name: "shell_execute", Description: "Execute a POSIX command using an authorized compute resource. Optional forward_ports publishes up to four unique TCP ports on the Molr machine, for both local and remote sandboxes. Use background=true or tty=true for servers. Bind the service to 0.0.0.0 or 127.0.0.1 in this execution. The receipt Task.Forwards contains actual Address and HTTP convenience URL; never invent a localhost or host port. Forward access is unauthenticated and reachable by anyone who can connect to that address; use only for user-requested services. Forwarding ends with the task and requires no outbound network grant. Foreground waits at most 60 seconds then automatically becomes background. background=true starts detached immediately; tty=true opens an interactive terminal (omit script for sh). tty and background are exclusive. Local TTY sources /work/.profile; remote TTY has no assistant profile. Ordinary commands do not load a profile. timeout_seconds is an optional runtime deadline, 0 means none. Background completion arrives as an event; no polling needed. Background/TTY running reminders arrive together every five minutes per assistant. Use shell_task to read, write, interrupt or stop. Isolated mode allows at most ten running executions per assistant; Full Access has no task quota. The following mount and network restrictions describe isolated mode. Call permission_list first: use its ResourceID, resource-relative Path and stable ShellPath for each requested file scope. Only requested resources are attached; overlapping scopes are rejected. User storage writes take effect immediately. Host-tools sandboxes reuse installed system commands read-only; check command availability. For HTTPS use curl with certificate verification and HTTPS-only redirects. Keep Python packages in a workspace venv; it requires host venv support. Use bounded search concurrency (fd --threads 2, rg --threads 2). The sandbox network gateway operates at layer 4 (the transport layer) and supports only TCP/UDP. Protocols outside this scope, such as ICMP, are not forwarded. Network resources explicitly declare TCP/UDP scope. Networking is transparent: use normal sockets and DNS, without proxy settings. host.molr.internal addresses authorized loopback services on the sandbox machine; localhost belongs to this execution. DNS returns execution-scoped virtual IPs; keep using hostnames and do not persist or share resolved IPs between executions. Network failure reasons distinguish denied destinations, DNS/connection failures, exhausted bytes and connection limits. Omitted sandbox_id always means local. Remote execution requires explicit sandbox_id; compute, file and network resources must belong to that node. Only local /work persists for the assistant. Remote computers have no /work; use authorized directory mounts or per-command /tmp. Other system paths are implementation details, not user-granted storage. Do not infer grants from filesystem permission bits. Runtime enforces isolation in isolated mode and rechecks access. Full Access is an additional compute permission (Scope.FullAccess), not a node mode. Only use full_access=true when explicitly needed and granted for this compute resource; it provides full filesystem and network access without isolation or quotas. For those calls use real paths (local calls may also use $MOLR_WORKSPACE) in scripts, omit files and network_mode. Ordinary calls use their separate isolated grant and never upgrade automatically. Runtime automatically selects this assistant’s active network grants in the selected sandbox; omit network_mode for automatic access, or use none for an offline execution. Failed commands may have side effects; do not blindly retry.", InputSchema: schema(`"sandbox_id":{"type":"string","description":"Omit for local; explicitly select a remote computer"},"full_access":{"type":"boolean","description":"Use an active Full Access compute grant for this call; default false preserves isolation"},"forward_ports":{"type":"array","maxItems":4,"uniqueItems":true,"items":{"type":"integer","minimum":1,"maximum":65535}},"tty":{"type":"boolean"},"background":{"type":"boolean"},"timeout_seconds":{"type":"integer","minimum":0,"maximum":604800},"compute_id":{"type":"string"},"script":{"type":"string","maxLength":65536},"directory":{"type":"string"},"files":{"type":"array","maxItems":32,"items":{"type":"object","properties":{"resource_id":{"type":"string"},"path":{"type":"string"},"write":{"type":"boolean"}},"required":["resource_id","path","write"],"additionalProperties":false}},"network_mode":{"enum":["auto","none"]}`, `"compute_id"`), RequiredActions: []string{"execute"}, SideEffect: true})
	}
	if e.options.ShellTask != nil {
		definitions = append(definitions, shellTaskDefinition())
	}
	definitions = append(definitions, e.fileDefinitions()...)
	if e.options.FileTransfer != nil {
		definitions = append(definitions, fileTransferDefinition())
	}
	definitions = append(definitions, e.planDefinitions()...)
	definitions = append(definitions, e.conversationDefinitions()...)
	return definitions
}
func (e *executor) authorize(ctx context.Context, p core.Principal, name string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if e.options.Authorizer == nil {
		return core.NewError(core.Unauthenticated, "authorization unavailable")
	}
	return e.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: core.ID(name)}, Action: policy.Execute})
}

// Catalog exposes only installed tool metadata to the owner. Unlike Definitions,
// it does not impersonate a running assistant or require tool execution rights.
func (e *executor) Catalog(ctx context.Context, p core.Principal) ([]Definition, error) {
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	if e.options.Authorizer == nil {
		return nil, core.NewError(core.Unauthenticated, "authorization unavailable")
	}
	if err := e.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: "catalog"}, Action: policy.Read}); err != nil {
		return nil, err
	}
	result := e.definitions()
	if e.options.ExtensionDefinitions != nil {
		extra, err := e.options.ExtensionDefinitions(ctx, p, true)
		if err != nil {
			return nil, err
		}
		result = append(result, extra...)
	}
	visible := result[:0]
	for _, d := range result {
		if err := e.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: core.ID(d.Name)}, Action: policy.Read}); err == nil {
			visible = append(visible, d)
		} else if core.CodeOf(err) != core.Forbidden && core.CodeOf(err) != core.NotFound {
			return nil, err
		}
	}
	return visible, nil
}

func (e *executor) Definitions(ctx context.Context, p core.Principal) ([]Definition, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if e.options.Authorizer == nil {
		return nil, core.NewError(core.Unauthenticated, "authorization unavailable")
	}
	if err := e.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "tool", ID: "catalog"}, Action: policy.Read}); err != nil {
		return nil, err
	}
	result := []Definition{}
	for _, d := range e.definitions() {
		if d.Name == "shell_execute" && e.options.ShellAvailable != nil {
			ok, err := e.options.ShellAvailable(ctx, p)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		if err := e.authorize(ctx, p, d.Name); err == nil {
			result = append(result, d)
		} else if code := core.CodeOf(err); code != core.Forbidden && code != core.NotFound {
			return nil, err
		}
	}
	if e.options.ExtensionDefinitions != nil {
		extra, err := e.options.ExtensionDefinitions(ctx, p, false)
		if err != nil {
			return nil, err
		}
		result = append(result, extra...)
	}
	return result, nil
}
func parse(call Call) (arguments, error) {
	var args arguments
	if call.Name == "file_transfer" {
		r, err := parseFileTransfer(call.Arguments)
		args.fileTransfer = &r
		return args, err
	}

	if strings.HasPrefix(call.Name, "plan_") {
		return args, validatePlanArguments(call)
	}
	if strings.HasPrefix(call.Name, "conversation_") {
		return args, validateConversationArguments(call)
	}
	if call.Name == "file_search" || call.Name == "file_read" || call.Name == "file_list" || call.Name == "file_write" || call.Name == "file_edit" {
		return fileArguments(call)
	}
	if call.Name == "profile_update" {
		r, err := parseProfile(call.Arguments)
		args.profile = &r
		return args, err
	}
	if call.Name == "permission_request" {
		_, err := parsePermissionRequest(call.Arguments)
		return args, err
	}
	if call.Name == "permission_list" {
		var object map[string]json.RawMessage
		if json.Unmarshal(call.Arguments, &object) != nil || object == nil || len(object) != 0 {
			return args, core.Invalid("arguments", "empty object required")
		}
		return args, nil
	}
	if call.Name == "shell_task" {
		r, err := ParseShellTask(call.Arguments)
		args.shellTask = &r
		return args, err
	}
	if call.Name == "shell_execute" {
		r, err := parseShell(call.Arguments)
		args.shell = &r
		args.ResourceID = r.ComputeID
		return args, err
	}
	return args, core.Invalid("tool", "unknown tool arguments")
}

func (e *executor) execute(ctx context.Context, call Call) (_ Result, runErr error) {
	if err := call.Execution.Validate(); err != nil {
		return Result{}, err
	}
	var definition *Definition
	definitions := e.definitions()
	if strings.HasPrefix(call.Name, "plg_") && e.options.ExtensionDefinitions != nil {
		extra, err := e.options.ExtensionDefinitions(ctx, call.Execution.Principal, false)
		if err != nil {
			return Result{}, err
		}
		definitions = append(definitions, extra...)
	}
	for _, d := range definitions {
		if d.Name == call.Name {
			copy := d
			definition = &copy
			break
		}
	}
	if definition == nil {
		return Result{}, core.NewError(core.NotFound, "unknown built-in tool")
	}
	authorizeName := call.Name
	if strings.HasPrefix(call.Name, "plg_") {
		authorizeName = call.Name
	}
	if err := e.authorize(ctx, call.Execution.Principal, authorizeName); err != nil {
		return Result{}, err
	}
	var args arguments
	var err error
	if strings.HasPrefix(call.Name, "plg_") && e.options.ExtensionValidate != nil {
		err = e.options.ExtensionValidate(ctx, call)
	} else {
		args, err = parse(call)
	}
	if err != nil {
		return Result{}, err
	}
	if args.ResourceID != "" {
		action := policy.Read
		if definition.SideEffect {
			action = policy.Write
		}
		if call.Name == "shell_execute" {
			action = policy.Execute
		}
		if err = e.options.Authorizer.Require(ctx, call.Execution.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: args.ResourceID}, ResourceID: args.ResourceID, Action: action, Destination: "/" + strings.TrimPrefix(args.Path, "/")}); err != nil {
			return Result{}, err
		}
		if call.Name == "file_edit" {
			if err = e.options.Authorizer.Require(ctx, call.Execution.Principal, policy.CheckRequest{Target: core.Object{Kind: "resource", ID: args.ResourceID}, ResourceID: args.ResourceID, Action: policy.Read, Destination: args.Path}); err != nil {
				return Result{}, err
			}
		}
	}
	if e.options.Operations == nil {
		return Result{}, core.NewError(core.FailedPrecondition, "operation recording required")
	}
	sum := sha256.Sum256(call.Arguments)
	var op operation.Operation
	cached := false
	err = e.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		op, err = e.options.Operations.Begin(ctx, operation.BeginRequest{Mutation: core.Mutation{IdempotencyKey: string(call.Execution.OperationID)}, Execution: call.Execution, ResourceID: args.ResourceID, Action: call.Name, InputDigest: hex.EncodeToString(sum[:])})
		if err != nil {
			return err
		}
		if op.State == operation.Succeeded {
			cached = true
			return nil
		}
		if op.State == operation.Failed {
			code := op.FailureCode
			if code == "" {
				code = core.FailedPrecondition
			}
			return core.NewError(code, "this operation already failed; correct the request before making a new call")
		}
		if op.State == operation.Canceled {
			return core.NewError(core.Canceled, "this operation was canceled; inspect its outcome before repeating it")
		}
		if op.State != operation.Prepared {
			return core.NewError(core.FailedPrecondition, "operation already running or unresolved")
		}
		if e.options.CheckGroupQuota != nil {
			if err := e.options.CheckGroupQuota(ctx, call); err != nil {
				return err
			}
		}
		if strings.HasPrefix(call.Name, "plg_") && e.options.ExtensionPrepare != nil {
			_, err := e.options.ExtensionPrepare(ctx, call)
			if err != nil {
				return err
			}
		}
		version := op.Version
		op.State = operation.Running
		op.Version++
		return operation.NewRepository(e.store).Save(ctx, op, version)
	})
	if err != nil {
		return Result{}, err
	}
	if cached {
		return resultWithMedia(call.Name, op.ID, op.Result), nil
	}
	// A local call is no longer running once dispatch returns, even when its
	// result could not be persisted. Do not leave its concurrency slot occupied.
	defer func() {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5e9)
		defer cancel()
		current, err := operation.NewRepository(e.store).Get(finishCtx, call.Execution.Principal.OwnerID, op.ID)
		if err == nil && current.State == operation.Running {
			err = e.options.Operations.MarkUnknown(finishCtx, call.Execution, op.ID)
		}
		if err != nil {
			runErr = errors.Join(runErr, err)
		}
	}()
	content, err := e.dispatch(ctx, call, args)
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5e9)
	defer cancel()
	if err != nil {
		// File mutations reject these errors before commit; plan mutations run
		// in one transaction. Other side effects remain unknown on failure.
		rejected := strings.HasPrefix(call.Name, "plg_") && core.ReasonOf(err) == "plugin_http_failed"
		if call.Name == "profile_update" || call.Name == "file_write" || call.Name == "file_edit" || call.Name == "file_transfer" || call.Name == "conversation_attachment" || (strings.HasPrefix(call.Name, "plan_") || call.Name == "permission_request") {
			switch core.CodeOf(err) {
			case core.Conflict, core.InvalidArgument, core.Forbidden, core.NotFound, core.FailedPrecondition:
				rejected = true
			}
		}
		if definition.SideEffect && !rejected {
			_ = e.options.Operations.MarkUnknown(persistCtx, call.Execution, op.ID)
		} else {
			_, _ = e.options.Operations.Complete(persistCtx, call.Execution, operation.Completion{ID: op.ID, ExpectedVersion: op.Version, State: operation.Failed, FailureCode: core.CodeOf(err)})
		}
		return Result{}, err
	}
	if len(content) > 64<<20 || !json.Valid(content) {
		_ = e.options.Operations.MarkUnknown(persistCtx, call.Execution, op.ID)
		return Result{}, core.NewError(core.Internal, "invalid tool output")
	}
	_, err = e.options.Operations.Complete(persistCtx, call.Execution, operation.Completion{ID: op.ID, ExpectedVersion: op.Version, State: operation.Succeeded, SafeResult: content})
	if err != nil {
		return Result{}, err
	}
	return resultWithMedia(call.Name, op.ID, content), nil
}
func (e *executor) dispatch(ctx context.Context, call Call, args arguments) (json.RawMessage, error) {
	if strings.HasPrefix(call.Name, "plg_") && e.options.ExtensionExecute != nil {
		return e.options.ExtensionExecute(ctx, call)
	}
	switch call.Name {
	case "profile_update":
		return e.options.ProfileUpdate(ctx, call.Execution, *args.profile)
	case "shell_task":
		return e.options.ShellTask(ctx, call.Execution, *args.shellTask)
	case "shell_execute":
		return e.options.ShellExecute(ctx, call.Execution, *args.shell)
	case "permission_request":
		r, err := parsePermissionRequest(call.Arguments)
		if err != nil {
			return nil, err
		}
		return e.options.PermissionRequest(ctx, call.Execution, r)
	case "permission_list":
		return e.options.PermissionList(ctx, call.Execution.Principal)
	case "conversation_read", "conversation_publish", "conversation_attachment":
		return e.conversationDispatch(ctx, call)
	case "plan_list", "plan_read", "plan_manage":
		return e.options.Plan(ctx, call.Execution, call.Name, call.Arguments)
	case "file_search":
		return e.options.FileSearch(ctx, call.Execution, *args.fileSearch)
	case "file_transfer":
		return e.options.FileTransfer(ctx, call.Execution, *args.fileTransfer)
	case "file_read":
		args.fileRead.ModelID = call.ModelID
		return e.options.FileRead(ctx, call.Execution, *args.fileRead)
	case "file_list":
		return e.options.FileList(ctx, call.Execution, *args.fileList)
	case "file_write":
		return e.options.FileWrite(ctx, call.Execution, *args.fileWrite)
	case "file_edit":
		return e.options.FileEdit(ctx, call.Execution, *args.fileEdit)

	}
	return nil, core.NewError(core.NotFound, "unknown built-in tool")
}
func (e *executor) Cancel(ctx context.Context, execution core.Execution, id core.ID) error {
	if err := execution.Validate(); err != nil {
		return err
	}
	if e.options.Operations == nil {
		return core.NewError(core.FailedPrecondition, "operation recording required")
	}
	op, err := e.options.Operations.Get(ctx, execution.Principal, id)
	if err != nil {
		return err
	}
	if op.ExecutionID != execution.Principal.ExecutionID || op.AssistantID != execution.AssistantID || op.ID != execution.OperationID {
		return core.NewError(core.Forbidden, "cancellation identity mismatch")
	}
	action := op.Action
	if strings.HasPrefix(action, "plg_") {
		action = "catalog"
	}
	if err = e.authorize(ctx, execution.Principal, action); err != nil {
		return err
	}
	if e.options.Cancel != nil {
		if err = e.options.Cancel(ctx, execution, id); err != nil {
			return err
		}
	}
	_, err = e.options.Operations.Cancel(ctx, execution.Principal, id)
	return err
}

// Only the trusted file reader may attach media. Other tool JSON stays text.
func resultWithMedia(name string, id core.ID, content json.RawMessage) Result {
	r := Result{OperationID: id, Content: content}
	if name == "file_read" {
		var out struct{ Media []media.Part }
		if json.Unmarshal(content, &out) == nil {
			r.Media = out.Media
			if len(out.Media) > 0 {
				var fields map[string]json.RawMessage
				if json.Unmarshal(content, &fields) == nil {
					delete(fields, "Media")
					r.Content, _ = json.Marshal(fields)
				}
			}
		}
	}
	return r
}
