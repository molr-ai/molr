package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	modelapi "molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/runtimeclient"
)

func (m *model) approvalsScreen(id core.ID) tea.Cmd {
	return m.loadScreen("待审批申请", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var items []policy.PermissionRequest
		if err := c.Do(ctx, "GET", "/api/permission-requests?assistant_id="+string(id), nil, &items); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "待审批申请", body: "选择申请查看完整范围；提交始终绑定当前展示的 ID 与版本。"}
		for _, q := range items {
			if q.State == "pending" {
				s.rows = append(s.rows, manageRow{q.Input.ResourceDescription + " · " + q.Input.Reason, func(m *model) tea.Cmd { return m.requestScreen(q) }})
			}
		}
		if len(s.rows) == 0 {
			s.body = "没有待处理申请"
		}
		return s, nil
	})
}
func resolutionText(r policy.PermissionResolution) string {
	text := fmt.Sprintf("资源：%s\n操作：%v\n模型：%s\n目录：%s", r.ResourceID, r.Actions, r.Scope.ModelID, strings.Join(r.Scope.PathPrefixes, "; "))
	if r.Scope.Once {
		text += "\n使用次数：仅执行一次"
	}
	if r.Scope.FullAccess {
		raw, _ := json.Marshal(r.Scope.FullAccessScope)
		text += "\n完全访问范围：" + string(raw) + "\n获准程序以服务账户权限执行，不受 Molr 沙盒限制"
	}
	if r.ExpiresAt != nil {
		text += "\n有效期至：" + r.ExpiresAt.Local().Format(time.DateTime)
	} else {
		text += "\n有效期：不设到期时间"
	}
	text += constraintsText(r.Constraints)
	if r.Temporary != nil {
		text += "\n临时额度到期：" + r.Temporary.Until.Local().Format(time.DateTime) + constraintsText(&r.Temporary.Limits)
	}
	return text
}
func constraintsText(c *policy.Constraints) string {
	if c == nil {
		return ""
	}
	if v := c.Compute; v != nil {
		return fmt.Sprintf("\n计算：CPU %d%%，内存 %d 字节，进程 %d，临时目录 %d 字节，时长 %d 秒，无时限 %t，GPU %v", v.CPUPercent, v.MemoryBytes, v.Processes, v.TempBytes, v.MaxDurationSeconds, v.UnlimitedDuration, v.GPUDevices())
	}
	if v := c.Network; v != nil {
		return fmt.Sprintf("\n网络：%d 字节，%d 连接，周期 %s，时区 %s", v.Bytes, v.Connections, v.Period, v.Timezone)
	}
	if v := c.Storage; v != nil {
		return fmt.Sprintf("\n存储：容量 %d 字节，单文件 %d 字节，文件数 %d", v.CapacityBytes, v.MaxFileBytes, v.MaxFiles)
	}
	return ""
}
func (m *model) requestScreen(q policy.PermissionRequest) tea.Cmd {
	proposals := []policy.PermissionResolution{approveInput(q.Input)}
	for _, v := range q.Input.Additional {
		proposals = append(proposals, approveInput(v))
	}
	return m.requestReview(q, proposals)
}
func (m *model) requestReview(q policy.PermissionRequest, proposals []policy.PermissionResolution) tea.Cmd {
	body := "申请 " + string(q.ID) + "\n" + q.Input.Reason
	for i, r := range proposals {
		body += fmt.Sprintf("\n\n%d. %s", i+1, resolutionText(r))
	}
	s := manageScreen{title: "审批详情", body: body}
	op := mutation(q.Version)
	s.rows = append(s.rows, manageRow{"按以上范围批准", func(m *model) tea.Cmd {
		return m.confirmAction("批准授权", body, "批准授权", func(ctx context.Context, c *runtimeclient.Client) error {
			r := proposals[0]
			r.Mutation = op
			r.RequestID = q.ID
			r.Decision = "approve"
			r.Additional = slices.Clone(proposals[1:])
			return c.Do(ctx, "POST", "/api/permission-requests/resolve", r, nil)
		}, "/approval")
	}})
	inputs := append([]policy.PermissionRequestInput{q.Input}, q.Input.Additional...)
	for i, input := range inputs {
		s.rows = append(s.rows, manageRow{fmt.Sprintf("调整第 %d 项范围／额度", i+1), func(m *model) tea.Cmd {
			return m.chooseResource(input.ResourceKind, proposals[i].ResourceID, func(m *model, r resource.Resource) tea.Cmd {
				return m.permissionForm(r, proposals[i], func(m *model, value policy.PermissionResolution) tea.Cmd {
					copy := slices.Clone(proposals)
					copy[i] = value
					return m.requestReview(q, copy)
				}, nil)
			})
		}})
	}
	s.rows = append(s.rows, manageRow{"拒绝", func(m *model) tea.Cmd {
		return m.confirmAction("拒绝申请", q.Input.Reason, "拒绝", func(ctx context.Context, c *runtimeclient.Client) error {
			return c.Do(ctx, "POST", "/api/permission-requests/resolve", policy.PermissionResolution{Mutation: op, RequestID: q.ID, Decision: "reject"}, nil)
		}, "/approval")
	}})
	return m.showScreen(s)
}
func (m *model) permissionsScreen(id core.ID) tea.Cmd {
	return m.loadScreen("资源授权", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		items, err := getAll[policy.Grant](ctx, c, "/api/grants?assistant_id="+string(id))
		if err != nil {
			return manageScreen{}, err
		}
		resources, err := getAll[resource.Resource](ctx, c, "/api/resources")
		if err != nil {
			return manageScreen{}, err
		}
		names := map[core.ID]string{}
		for _, r := range resources {
			names[r.ID] = r.Name
		}
		s := manageScreen{title: "资源授权", rows: []manageRow{{"新增授权", func(m *model) tea.Cmd {
			return m.chooseResource("", "", func(m *model, r resource.Resource) tea.Cmd { return m.grantForm(id, r, nil) })
		}}}}
		for _, g := range items {
			if g.RevokedAt != nil {
				continue
			}
			label := names[g.ResourceID]
			if label == "" {
				label = string(g.ResourceID)
			}
			if g.ExpiresAt != nil && !g.ExpiresAt.After(time.Now()) {
				label += "（已到期）"
			}
			s.rows = append(s.rows, manageRow{label + " · " + fmt.Sprint(g.Actions), func(m *model) tea.Cmd { return m.grantScreen(g) }})
		}
		return s, nil
	})
}
func (m *model) grantScreen(g policy.Grant) tea.Cmd {
	r := policy.PermissionResolution{ResourceID: g.ResourceID, Actions: g.Actions, Scope: g.Scope, ExpiresAt: g.ExpiresAt, Constraints: g.Constraints, Temporary: g.Temporary}
	op := mutation(g.Version)
	s := manageScreen{title: "授权详情", body: resolutionText(r), rows: []manageRow{{"调整授权", func(m *model) tea.Cmd {
		return m.loadScreen("调整授权", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
			var resource resource.Resource
			if err := c.Do(ctx, "GET", "/api/resources/"+string(g.ResourceID), nil, &resource); err != nil {
				return manageScreen{}, err
			}
			return manageScreen{title: "调整授权", rows: []manageRow{{"编辑范围与额度", func(m *model) tea.Cmd { return m.grantForm(g.AssistantID, resource, &g) }}}}, nil
		})
	}}, {"撤销授权", func(m *model) tea.Cmd {
		return m.confirmAction("撤销授权", resolutionText(r)+"\n\n撤销可能中断正在使用此授权的任务。", "撤销", func(ctx context.Context, c *runtimeclient.Client) error {
			return c.Do(ctx, "POST", "/api/grants/revoke", policy.RevokeRequest{Mutation: op, GrantID: g.ID}, nil)
		}, "/permissions")
	}}}}
	return m.showScreen(s)
}
func (m *model) chooseResource(kind string, preferred core.ID, next func(*model, resource.Resource) tea.Cmd) tea.Cmd {
	return m.loadScreen("选择资源", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		items, err := getAll[resource.Resource](ctx, c, "/api/resources")
		if err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "选择资源", body: "只列出账号可见且当前可用的资源。"}
		for _, r := range items {
			if (kind == "" || string(r.Kind) == kind) && r.State == resource.Available {
				if r.ID == preferred {
					s.cursor = len(s.rows)
				}
				s.rows = append(s.rows, manageRow{r.Name + " · " + string(r.Kind), func(m *model) tea.Cmd { return next(m, r) }})
			}
		}
		if len(s.rows) == 0 {
			s.body = "没有可用资源，请管理员检查资源连接与账号分配。"
		}
		return s, nil
	})
}
func (m *model) grantForm(id core.ID, r resource.Resource, g *policy.Grant) tea.Cmd {
	p := policy.PermissionResolution{ResourceID: r.ID}
	if g != nil {
		p = policy.PermissionResolution{ResourceID: g.ResourceID, Actions: g.Actions, Scope: g.Scope, ExpiresAt: g.ExpiresAt, Constraints: g.Constraints, Temporary: g.Temporary}
	}
	op := mutation(0)
	if g != nil {
		op.ExpectedVersion = g.Version
	}
	return m.permissionForm(r, p, nil, func(v policy.PermissionResolution) func(context.Context, *runtimeclient.Client) error {
		return func(ctx context.Context, c *runtimeclient.Client) error {
			if g != nil {
				return c.Do(ctx, "POST", "/api/grants/update", policy.UpdateGrantRequest{Mutation: op, GrantID: g.ID, Actions: v.Actions, Scope: v.Scope, ExpiresAt: v.ExpiresAt, Constraints: v.Constraints, Temporary: v.Temporary}, nil)
			}
			return c.Do(ctx, "POST", "/api/grants", policy.GrantRequest{Mutation: op, AssistantID: id, ResourceID: r.ID, Actions: v.Actions, Scope: v.Scope, ExpiresAt: v.ExpiresAt, Constraints: v.Constraints, Temporary: v.Temporary}, nil)
		}
	})
}
func (m *model) permissionForm(r resource.Resource, p policy.PermissionResolution, local func(*model, policy.PermissionResolution) tea.Cmd, save func(policy.PermissionResolution) func(context.Context, *runtimeclient.Client) error) tea.Cmd {
	return m.loadScreen("授权范围", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		if p.ResourceID != r.ID {
			p.Scope = policy.Scope{}
			p.Actions = nil
			p.Constraints = nil
			p.Temporary = nil
		}
		p.ResourceID = r.ID
		if len(p.Actions) == 0 {
			if slices.Contains(r.Actions, "read") {
				p.Actions = []policy.Action{policy.Read}
			} else {
				p.Actions = []policy.Action{policy.Execute}
			}
		}
		actions := []string{}
		for _, a := range p.Actions {
			actions = append(actions, string(a))
		}
		expiry := ""
		if p.ExpiresAt != nil {
			expiry = p.ExpiresAt.Format(time.RFC3339)
		} else if p.Scope.FullAccess {
			expiry = "permanent"
		}
		fields := []formField{field("actions", "操作（分号分隔）："+strings.Join(r.Actions, ";"), strings.Join(actions, ";")), field("expires", "到期时间 RFC3339（完全访问必填；永久填 permanent）", expiry)}
		if r.Kind == resource.Storage {
			fields = append(fields, field("paths", "授权目录（分号分隔，/ 表示整个资源）", strings.Join(p.Scope.PathPrefixes, ";")))
		}
		if r.Kind == resource.Model {
			var models []modelapi.Model
			if err := c.Do(ctx, "GET", "/api/models", nil, &models); err != nil {
				return manageScreen{}, err
			}
			f := field("model", "模型", string(p.Scope.ModelID))
			for _, v := range models {
				if v.ResourceID == r.ID && v.Pricing != nil {
					f.choices = append(f.choices, string(v.ID))
					f.labels = append(f.labels, v.Name)
				}
			}
			if len(f.choices) == 0 {
				return manageScreen{}, fmt.Errorf("该资源没有价格已配置的可用模型")
			}
			if !slices.Contains(f.choices, f.input.Value()) {
				f.input.SetValue(f.choices[0])
			}
			fields = append(fields, f)
		}
		if r.Kind == resource.Compute {
			fields = append(fields, field("full", "完全访问（绕过 Molr 隔离与配额）", strconv.FormatBool(p.Scope.FullAccess), "false", "true"))
			fields = append(fields, field("once", "仅执行一次", strconv.FormatBool(p.Scope.Once), "false", "true"))
			mode, rules := "", "[]"
			if p.Scope.FullAccessScope != nil {
				mode = p.Scope.FullAccessScope.Mode
				data, _ := json.Marshal(p.Scope.FullAccessScope.Programs)
				rules = string(data)
			}
			fields = append(fields, field("full_mode", "完全访问范围（必须明确选择）", mode, "", "programs", "unrestricted"), field("full_programs", "程序规则 JSON（executable、arguments:any/exact、args）", rules))
		}
		// Use node capabilities for native duration-only compute and network toggles.
		var nodes []struct {
			ID   core.ID
			Info struct {
				Capabilities struct {
					ResourceLimits bool
					NetworkBackend string
				}
			}
		}
		native, networkToggle := false, false
		if r.Kind == resource.Compute || r.Kind == resource.Network {
			if err := c.Do(ctx, "GET", "/api/sandboxes", nil, &nodes); err != nil {
				return manageScreen{}, err
			}
			for _, n := range nodes {
				if n.ID == r.SandboxID {
					native = !n.Info.Capabilities.ResourceLimits
					networkToggle = strings.HasSuffix(n.Info.Capabilities.NetworkBackend, "-toggle")
				}
			}
		}
		constraints := p.Constraints
		needs := r.Kind == resource.Compute || r.Kind == resource.Network && !networkToggle || r.Kind == resource.Storage && r.Provider == "managed-workspace"
		if constraints == nil && needs {
			constraints = defaultConstraints(string(r.Kind), native)
		}
		fields = append(fields, constraintFields("base.", constraints)...)
		temporaryUntil := ""
		if p.Temporary != nil {
			temporaryUntil = p.Temporary.Until.Format(time.RFC3339)
		}
		if constraints != nil {
			fields = append(fields, field("temporary", "临时额度到期 RFC3339（空为关闭）", temporaryUntil))
			temporary := constraints
			if p.Temporary != nil {
				temporary = &p.Temporary.Limits
			}
			fields = append(fields, constraintFields("temp.", temporary)...)
		}
		s := manageScreen{title: "授权范围", body: r.Name + "\n修改前核对操作、目录、时长和额度。临时额度字段仅在填写到期时间后生效。", fields: fields, submitLabel: "检查最终范围", returnTo: "/permissions"}
		parse := func(v map[string]string) (policy.PermissionResolution, error) {
			out := p
			out.Actions = nil
			for _, a := range splitValues(v["actions"]) {
				if !slices.Contains(r.Actions, a) {
					return out, fmt.Errorf("资源不支持操作 %s", a)
				}
				out.Actions = append(out.Actions, policy.Action(a))
			}
			if len(out.Actions) == 0 {
				return out, fmt.Errorf("至少选择一个操作")
			}
			out.Scope.ReadOnly = !slices.Contains(out.Actions, policy.Write) && !slices.Contains(out.Actions, policy.Execute)
			if r.Kind == resource.Model {
				out.Scope.ModelID = core.ID(v["model"])
			}
			if r.Kind == resource.Storage {
				out.Scope.PathPrefixes = splitValues(v["paths"])
				if len(out.Scope.PathPrefixes) == 0 {
					return out, fmt.Errorf("请明确填写授权目录")
				}
			}
			out.ExpiresAt = nil
			if v["full"] == "true" && v["expires"] == "" {
				return out, fmt.Errorf("完全访问必须明确有效期；永久请填写 permanent")
			}
			if v["expires"] != "" && v["expires"] != "permanent" {
				at, err := time.Parse(time.RFC3339, v["expires"])
				if err != nil || !at.After(time.Now()) {
					return out, fmt.Errorf("到期时间必须是未来的 RFC3339 时间")
				}
				out.ExpiresAt = &at
			}
			out.Scope.FullAccess = v["full"] == "true"
			if v["once"] == "true" && !out.Scope.FullAccess {
				return out, fmt.Errorf("仅执行一次需要完全访问授权")
			}
			out.Scope.Once = out.Scope.FullAccess && v["once"] == "true"
			out.Scope.FullAccessScope = nil
			if out.Scope.FullAccess {
				scope := &policy.FullAccessScope{Mode: v["full_mode"]}
				if scope.Mode == "programs" {
					if err := json.Unmarshal([]byte(v["full_programs"]), &scope.Programs); err != nil {
						return out, fmt.Errorf("程序规则 JSON 无效")
					}
				}
				if err := scope.Validate(); err != nil {
					return out, err
				}
				out.Scope.FullAccessScope = scope
			}
			out.Constraints = nil
			out.Temporary = nil
			if !out.Scope.FullAccess {
				var err error
				out.Constraints, err = parseConstraints("base.", constraints, v)
				if err != nil {
					return out, err
				}
				if v["temporary"] != "" {
					at, err := time.Parse(time.RFC3339, v["temporary"])
					if err != nil || !at.After(time.Now()) || out.ExpiresAt != nil && at.After(*out.ExpiresAt) {
						return out, fmt.Errorf("临时额度到期时间必须在授权期限内")
					}
					limits, err := parseConstraints("temp.", constraints, v)
					if err != nil {
						return out, err
					}
					out.Temporary = &policy.TemporaryConstraints{Limits: *limits, Until: at}
				}
			} else {
				out.Actions = []policy.Action{policy.Execute}
				out.Scope.ReadOnly = false
			}
			return out, nil
		}
		s.localSubmit = func(m *model, v map[string]string) tea.Cmd {
			out, err := parse(v)
			if err != nil {
				m.manage.screen.notice = err.Error()
				return nil
			}
			if local != nil {
				return local(m, out)
			}
			return m.confirmAction("确认授权", resolutionText(out), "保存授权", save(out), "/permissions")
		}
		return s, nil
	})
}
func defaultConstraints(kind string, native bool) *policy.Constraints {
	switch kind {
	case "compute":
		v := &policy.ComputeConstraints{CPUPercent: 100, MemoryBytes: 256 << 20, Processes: 64, TempBytes: 64 << 20, MaxDurationSeconds: 600}
		if native {
			v.CPUPercent = 0
			v.MemoryBytes = 0
			v.Processes = 0
			v.TempBytes = 0
		}
		return &policy.Constraints{Compute: v}
	case "network":
		return &policy.Constraints{Network: &policy.NetworkConstraints{Bytes: 100 << 20, Connections: 16, Period: "day", Timezone: "UTC"}}
	case "storage":
		return &policy.Constraints{Storage: &policy.StorageConstraints{CapacityBytes: 1 << 30, MaxFileBytes: 20 << 20, MaxFiles: 1000}}
	}
	return nil
}
func constraintFields(prefix string, c *policy.Constraints) []formField {
	var f []formField
	if c == nil {
		return f
	}
	name := ""
	if prefix == "temp." {
		name = "临时 "
	}
	add := func(k, label string, v int64) { f = append(f, field(prefix+k, name+label, strconv.FormatInt(v, 10))) }
	if v := c.Compute; v != nil {
		add("cpu", "CPU 百分比（原生时长模式为 0）", int64(v.CPUPercent))
		add("memory", "内存字节", v.MemoryBytes)
		add("processes", "进程数", int64(v.Processes))
		add("temp", "临时目录字节", v.TempBytes)
		add("duration", "执行时长秒", v.MaxDurationSeconds)
		f = append(f, field(prefix+"unlimited", name+"不限制执行时长", strconv.FormatBool(v.UnlimitedDuration), "false", "true"), field(prefix+"gpu", name+"GPU UUID（分号分隔）", strings.Join(v.GPUDevices(), ";")))
	}
	if v := c.Network; v != nil {
		add("bytes", "流量字节", v.Bytes)
		add("connections", "连接数", int64(v.Connections))
		f = append(f, field(prefix+"period", name+"流量周期", v.Period, "lifetime", "day", "week", "month"), field(prefix+"timezone", name+"时区", v.Timezone))
	}
	if v := c.Storage; v != nil {
		add("capacity", "总容量字节", v.CapacityBytes)
		add("file", "单文件字节", v.MaxFileBytes)
		add("files", "文件数量", v.MaxFiles)
	}
	return f
}
func parseConstraints(prefix string, base *policy.Constraints, v map[string]string) (*policy.Constraints, error) {
	if base == nil {
		return nil, nil
	}
	c := &policy.Constraints{}
	var bad error
	n := func(k string) int64 {
		x, e := strconv.ParseInt(v[prefix+k], 10, 64)
		if e != nil || x < 0 {
			bad = fmt.Errorf("%s 请输入非负整数", k)
		}
		return x
	}
	if base.Compute != nil {
		c.Compute = &policy.ComputeConstraints{CPUPercent: int(n("cpu")), MemoryBytes: n("memory"), Processes: int(n("processes")), TempBytes: n("temp"), MaxDurationSeconds: n("duration"), UnlimitedDuration: v[prefix+"unlimited"] == "true"}
		if c.Compute.UnlimitedDuration {
			c.Compute.MaxDurationSeconds = 0
		}
		if gpu := splitValues(v[prefix+"gpu"]); len(gpu) > 0 {
			c.Compute.GPU = &core.GPUAccess{DeviceIDs: gpu}
		}
	}
	if base.Network != nil {
		c.Network = &policy.NetworkConstraints{Bytes: n("bytes"), Connections: int(n("connections")), Period: v[prefix+"period"], Timezone: v[prefix+"timezone"]}
	}
	if base.Storage != nil {
		c.Storage = &policy.StorageConstraints{CapacityBytes: n("capacity"), MaxFileBytes: n("file"), MaxFiles: n("files")}
	}
	if bad != nil {
		return nil, bad
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("额度不完整或不符合限制：%s", err)
	}
	return c, nil
}
func (m *model) resourceScreen(id core.ID) tea.Cmd {
	return m.loadScreen("资源详情", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var r resource.Resource
		if err := c.Do(ctx, "GET", "/api/resources/"+string(id), nil, &r); err != nil {
			return manageScreen{}, err
		}
		return manageScreen{title: "资源详情", body: fmt.Sprintf("%s\n类型：%s\n状态：%s\n支持操作：%s", r.Name, r.Kind, r.State, strings.Join(r.Actions, ", "))}, nil
	})
}

func (m *model) requestByIDScreen(id core.ID) tea.Cmd {
	return m.loadScreen("授权申请", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var q policy.PermissionRequest
		if err := c.Do(ctx, "GET", "/api/permission-requests/"+string(id), nil, &q); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "授权申请", body: q.Input.Reason + "\n状态：" + q.State}
		if q.State == "pending" {
			s.rows = []manageRow{{"查看并审批此申请", func(m *model) tea.Cmd { return m.requestScreen(q) }}}
		}
		return s, nil
	})
}

func approveInput(input policy.PermissionRequestInput) policy.PermissionResolution {
	return policy.PermissionResolution{ResourceID: input.ResourceID, Actions: input.Actions, Scope: policy.Scope{ReadOnly: !slices.Contains(input.Actions, policy.Write) && !slices.Contains(input.Actions, policy.Execute), FullAccess: input.FullAccess, Once: input.Once, FullAccessScope: input.FullAccessScope, ModelID: input.ModelID, PathPrefixes: input.PathPrefixes}, ExpiresAt: input.ExpiresAt, Constraints: input.Constraints, Temporary: input.Temporary, GrantID: input.GrantID, GrantVersion: input.GrantVersion}
}
