package prompt

var scope = object("ModelID ReadOnly FullAccess", nested("PathPrefixes", array(scalar)))
var constraints = object("", nested("compute", object("cpu_percent memory_bytes processes max_duration_seconds unlimited_duration temp_bytes", nested("gpu", object("", nested("device_ids", array(scalar)))))), nested("network", object("bytes connections period timezone")), nested("storage", object("capacity_bytes max_file_bytes max_files")))
var temporary = object("until", nested("limits", constraints))
var grant = object("ID ResourceID Name Kind Version", nested("Constraints", constraints), nested("Temporary", temporary), required("ExpiresAt", scalar), nested("Actions", array(scalar)), nested("Scope", scope))
var requestInput = object("full_access resource_kind resource_id model_id resource_description expires_at reason grant_id grant_version", nested("constraints", constraints), nested("temporary", temporary), nested("actions", array(scalar)), nested("path_prefixes", array(scalar)))
var request = object("ID State GrantID MessageID CreatedAt ResolvedAt", nested("Input", requestInput), nested("Granted", grant))
var profile = object("Name Version", nested("Avatar", object("Type ID Digest")))
var failure = object("code reason field message execution_state side_effects_possible guidance", nested("current_profile", profile))
var file = object("ID Name MediaType Size State Path Version")
var message = object("ID ConversationID Sequence Sender CreatedAt", nested("Content", object("Kind", required("Text", scalar), nested("Card", object("Kind TargetID Title Summary")), nested("AttachmentIDs", array(scalar)))))
var operation = object("Action Via Available Reason")
var capabilities = object("FullAccess Shell ProcessIsolation FileIsolation NetworkIsolation ResourceLimits Reason")
var shell = object("ID TaskID SandboxID OutputSandboxID FullAccess Mode State Script Failure StartedAt FinishedAt Stdout Stderr StdoutPath StderrPath Output NextCursor Path Stopping Bytes Truncated OutputComplete CaptureReason StdoutBytes StderrBytes", nested("GPUDeviceIDs", array(scalar)), nested("Forwards", array(object("Protocol Port Address URL State"))), nested("Exit", object("NetworkReason Reason Code TimedOut OutputTruncated")))
var execution = object("ID State Title Instructions PlanVersion OccurrenceAt StartedAt CompletedAt Result")
var plan = object("PlanID Mode Title Instructions Version StartAt EndAt DueAt Timezone Recurrence MissedPolicy MaxCatchUp Enabled Handoff Paused Ready ExecutionCount ExecutionsTruncated InstructionsTruncated Deleted", nested("CurrentExecution", execution), nested("Dependencies", array(scalar)), nested("BlockedBy", array(scalar)), nested("Executions", array(execution)), nested("Occurrences", array(scalar)))
var networkTarget = object("", nested("Protocols", array(scalar)), nested("Addresses", array(scalar)), nested("Ports", array(scalar)), nested("ExcludeAddresses", array(scalar)), nested("ExcludePorts", array(scalar)))
var networkScope = object("Known Reason LoopbackAccess", nested("Target", networkTarget), nested("Restrictions", array(scalar)))
var quotaUsage = object("", nested("Constraints", constraints), nested("Network", object("UsedBytes ReservedBytes LimitBytes PeriodStart PeriodEnd Connections")), nested("Compute", object("MemoryBytes Processes CPUUsageMicroseconds")), nested("Storage", object("Supported Reason UsedBytes CapacityBytes Files MaxFiles")))
var quotaCeilings = object("CPUPercent MemoryBytes Processes TempBytes MaxFileBytes NetworkBytes NetworkConnections", nested("GPUDeviceIDs", array(scalar)))
var permissions = object("CurrentTime Truncated RequestableTruncated",
	nested("Sandboxes", array(object("ID Name Local Description Storage", nested("FullAccessComputeIDs", array(scalar)), nested("Capabilities", object("Shell FullAccess Reason"))))),
	required("Resources", array(object("FullAccess SandboxID ResourceID Name Kind Source Path PathMeaning FilePath ShellPath ModelID ModelName", nested("Usage", quotaUsage), nested("Network", networkScope), required("Grants", array(grant)), nested("Operations", array(operation))))),
	required("BuiltInStorage", array(object("SandboxID Kind Source Path Lifetime Available Reason", nested("Actions", array(scalar)), nested("Operations", array(operation)), nested("ShellComputeIDs", array(scalar))))),
	required("Requestable", array(object("ResourceID ResourceKind Kind Name SandboxID State ModelID ModelName Description Workspace WorkspaceQuotaSupported FullAccessAvailable", nested("AuthorizationCeilings", quotaCeilings), nested("GPUs", array(object("ID Name Vendor MemoryBytes Available Reason"))), nested("Network", networkScope), nested("Actions", array(scalar)), nested("PathPrefixes", array(scalar)), nested("Scope", scope)))),
	nested("Sandboxes", array(object("ID Name Default Storage Description", nested("FullAccessComputeIDs", array(scalar)), nested("Capabilities", capabilities)))))

var schemas = map[string]*shape{
	"workspace":               object("SandboxID Path Available Reason Lifetime", nested("Shared", object("Path Access Lifetime")), nested("Temporary", object("Path Access Lifetime")), nested("Directories", array(object("Path Purpose")))),
	"profile":                 profile,
	"profile_update":          object("Changed", nested("Profile", profile)),
	"user_preferences":        object("timezone language currency", nested("sources", object("timezone language currency"))),
	"permissions":             object("Total Truncated", required("Granted", array(grant))),
	"permission_requests":     array(request),
	"skill":                   object("name path frontmatter read_tool"),
	"event":                   object("ID Kind MessageID ScheduleID ScheduleVersion OccurrenceAt OperationID TaskID CreatedAt ExecutionID", nested("Details", object("ID Mode State Failure StartedAt FinishedAt StdoutPath StderrPath", nested("Exit", object("NetworkReason Reason Code TimedOut OutputTruncated")), nested("Tasks", array(shell))))),
	"failure":                 object("ok operation_id", nested("error", failure)),
	"conversation_read":       object("NextCursor", required("Items", array(message)), nested("Attachments", &shape{dictionary: file})),
	"conversation_publish":    object("ID ConversationID Sequence CreatedAt"),
	"conversation_attachment": object("Path Version", nested("File", file)),
	"file_transfer":           object("SourceVersion Version SHA256 Bytes", nested("Source", object("sandbox_id resource_id path")), nested("Destination", object("sandbox_id resource_id path"))),
	"file_read":               object("SandboxID ResourceID Path Version StartLine EndLine TotalLines NextOffset ByteOffset NextByteOffset Truncated path version media_type size_bytes width height duration_seconds content_loaded reason start_seconds end_seconds audio_included truncated", required("Text", scalar), nested("File", file)),
	"file_search":             object("SandboxID ResourceID Path TargetKind Complete Truncated StopReason EntriesScanned FilesScanned BytesScanned", nested("Skipped", &shape{dictionary: scalar}), required("Matches", array(object("Path Line Text Version", nested("Before", array(object("Line Text"))), nested("After", array(object("Line Text"))))))),
	"file_list":               object("SandboxID ResourceID Path Version NextOffset NextCursor Truncated", required("Entries", array(object("Name Path Type Size Version")))),
	"file_write":              object("SandboxID ResourceID Path Version Bytes"),
	"file_edit":               object("SandboxID ResourceID Path Version Bytes Replacements"),
	"file_move":               object("Path ToPath Version"),
	"file_delete":             object("Path Deleted"),
	"permission_list":         permissions,
	"permission_request":      request,
	"shell_execute":           shell,
	"shell_task":              object("Output NextCursor Path Stopping Accepted Started Bytes Limit", required("Items", array(shell))),
	"plan_list":               object("NextCursor", required("Items", array(plan))),
	"plan_read":               plan,
	"plan_manage":             plan,
}

func init() {
	entry := &shape{fields: append([]field{}, requestInput.fields...)}
	requestInput.fields = append(requestInput.fields, nested("additional_requests", array(entry)))
	request.fields = append(request.fields, nested("GrantedAll", array(grant)))
	shell.fields = append(shell.fields, nested("Result", object("Stdout Stderr Truncated OutputComplete StdoutPath StderrPath StdoutBytes StderrBytes", nested("Exit", object("NetworkReason Reason Code TimedOut OutputTruncated")))))
	schemas["shell_execute"] = &shape{fields: append(append([]field{}, shell.fields...), nested("Task", shell))}
	schemas["archive"] = object("full_result_path full_result_sandbox_id bytes", nested("details", object("SandboxID ok operation_id Path TargetKind Version NextOffset NextByteOffset Truncated Total TotalCount Count HasMore NextCursor OutputComplete StdoutPath StderrPath StdoutBytes StderrBytes", nested("Exit", object("NetworkReason Reason Code TimedOut OutputTruncated")), nested("error", failure))))
}
