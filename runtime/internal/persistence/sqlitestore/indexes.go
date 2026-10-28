package sqlitestore

import (
	"fmt"
	"strings"
)

// Names describe indexed expressions and stay stable when declarations move.
// Only this records_query_ family is reconciled; unique and other indexes remain
// independently managed. Reconciliation changes derived indexes, never records.
type recordIndex struct {
	name   string
	bucket string
	fields []string
}

var recordIndexes = []recordIndex{
	{"records_query_all_ownerid_key", "", []string{"OwnerID", "key"}},
	{"records_query_agent_events_ownerid_assistantid_key", "agent.events", []string{"OwnerID", "AssistantID", "key"}},
	{"records_query_agent_events_ownerid_assistantid_kind_key", "agent.events", []string{"OwnerID", "AssistantID", "Kind", "key"}},
	{"records_query_agent_jobs_event_kind_state_key", "agent.jobs", []string{"Event.Kind", "State", "key"}},
	{"records_query_agent_jobs_event_ownerid_event_assistantid_executionid", "agent.jobs", []string{"Event.OwnerID", "Event.AssistantID", "ExecutionID"}},
	{"records_query_agent_jobs_event_ownerid_event_assistantid_key", "agent.jobs", []string{"Event.OwnerID", "Event.AssistantID", "key"}},
	{"records_query_agent_jobs_event_ownerid_event_assistantid_state_key", "agent.jobs", []string{"Event.OwnerID", "Event.AssistantID", "State", "key"}},
	{"records_query_agent_jobs_event_ownerid_event_assistantid_state_queuekey", "agent.jobs", []string{"Event.OwnerID", "Event.AssistantID", "State", "QueueKey"}},
	{"records_query_agent_jobs_event_ownerid_joinedto_key", "agent.jobs", []string{"Event.OwnerID", "JoinedTo", "key"}},
	{"records_query_agent_jobs_event_ownerid_key", "agent.jobs", []string{"Event.OwnerID", "key"}},
	{"records_query_agent_jobs_event_ownerid_state_key", "agent.jobs", []string{"Event.OwnerID", "State", "key"}},
	{"records_query_agent_jobs_state_queuekey", "agent.jobs", []string{"State", "QueueKey"}},
	{"records_query_agent_model_retries_ownerid_assistantid_eventid_key", "agent.model_retries", []string{"OwnerID", "AssistantID", "EventID", "key"}},
	{"records_query_agent_operations_ownerid_action_createdat", "agent.operations", []string{"OwnerID", "Action", "CreatedAt"}},
	{"records_query_agent_operations_ownerid_action_createdatkey_state_id", "agent.operations", []string{"OwnerID", "Action", "CreatedAtKey", "State", "ID"}},
	{"records_query_agent_operations_ownerid_action_state", "agent.operations", []string{"OwnerID", "Action", "State"}},
	{"records_query_agent_operations_ownerid_action_state_createdatkey_id", "agent.operations", []string{"OwnerID", "Action", "State", "CreatedAtKey", "ID"}},
	{"records_query_agent_operations_ownerid_assistantid_executionid", "agent.operations", []string{"OwnerID", "AssistantID", "ExecutionID"}},
	{"records_query_agent_operations_ownerid_executionid_key", "agent.operations", []string{"OwnerID", "ExecutionID", "key"}},
	{"records_query_agent_operations_ownerid_idempotencykey", "agent.operations", []string{"OwnerID", "IdempotencyKey"}},
	{"records_query_agent_operations_state_key", "agent.operations", []string{"State", "key"}},
	{"records_query_auth_accounts_account_groupid_account_disabled_key", "auth.accounts", []string{"Account.GroupID", "Account.Disabled", "key"}},
	{"records_query_auth_accounts_account_username_key", "auth.accounts", []string{"Account.Username", "key"}},
	{"records_query_auth_challenges_accountid_key", "auth.challenges", []string{"AccountID", "key"}},
	{"records_query_auth_challenges_expiresatkey_key", "auth.challenges", []string{"ExpiresAtKey", "key"}},
	{"records_query_auth_login_links_accountid_createdatkey_link_id_key", "auth.login-links", []string{"AccountID", "CreatedAtKey", "Link.ID", "key"}},
	{"records_query_auth_login_links_accountid_link_id_key", "auth.login-links", []string{"AccountID", "Link.ID", "key"}},
	{"records_query_auth_login_links_expiresatkey_key", "auth.login-links", []string{"ExpiresAtKey", "key"}},
	{"records_query_auth_sessions_revokedat_expiresatkey_key", "auth.sessions", []string{"RevokedAt", "ExpiresAtKey", "key"}},
	{"records_query_auth_sessions_revokedat_lastseenatkey_key", "auth.sessions", []string{"RevokedAt", "LastSeenAtKey", "key"}},
	{"records_query_auth_sessions_revokedatkey_key", "auth.sessions", []string{"RevokedAtKey", "key"}},
	{"records_query_auth_sessions_session_accountid_devicedigest_key", "auth.sessions", []string{"Session.AccountID", "DeviceDigest", "key"}},
	{"records_query_auth_sessions_session_accountid_key", "auth.sessions", []string{"Session.AccountID", "key"}},
	{"records_query_auth_sessions_session_accountid_revokedat_lastseenatkey_desc_session_id_desc_key_desc", "auth.sessions", []string{"Session.AccountID", "RevokedAt", "LastSeenAtKey DESC", "Session.ID DESC", "key DESC"}},
	{"records_query_auth_sessions_tokendigest_key", "auth.sessions", []string{"TokenDigest", "key"}},
	{"records_query_auth_tokens_accountid_key", "auth.tokens", []string{"AccountID", "key"}},
	{"records_query_auth_tokens_digest_key", "auth.tokens", []string{"Digest", "key"}},
	{"records_query_business_assistants_ownerid_groupid_key", "business.assistants", []string{"OwnerID", "GroupID", "key"}},
	{"records_query_business_assistants_ownerid_state_key", "business.assistants", []string{"OwnerID", "State", "key"}},
	{"records_query_business_assistants_state_key", "business.assistants", []string{"State", "key"}},
	{"records_query_business_bindings_credentialid_key", "business.bindings", []string{"CredentialID", "key"}},
	{"records_query_business_dream_runs_ownerid_assistantid_createdatkey_key", "business.dream_runs", []string{"OwnerID", "AssistantID", "CreatedAtKey", "key"}},
	{"records_query_business_dream_runs_ownerid_assistantid_key", "business.dream_runs", []string{"OwnerID", "AssistantID", "key"}},
	{"records_query_business_dream_runs_ownerid_assistantid_state_key", "business.dream_runs", []string{"OwnerID", "AssistantID", "State", "key"}},
	{"records_query_business_dream_sessions_ownerid_assistantid_deadlinekey_key", "business.dream_sessions", []string{"OwnerID", "AssistantID", "DeadlineKey", "key"}},
	{"records_query_business_dream_sessions_ownerid_assistantid_endkey_key", "business.dream_sessions", []string{"OwnerID", "AssistantID", "EndKey", "key"}},
	{"records_query_business_dream_sessions_ownerid_assistantid_key", "business.dream_sessions", []string{"OwnerID", "AssistantID", "key"}},
	{"records_query_business_dream_sessions_ownerid_assistantid_state_deadlinekey_key", "business.dream_sessions", []string{"OwnerID", "AssistantID", "State", "DeadlineKey", "key"}},
	{"records_query_business_dream_sessions_ownerid_assistantid_state_key", "business.dream_sessions", []string{"OwnerID", "AssistantID", "State", "key"}},
	{"records_query_business_dream_sessions_ownerid_deadlinekey_key", "business.dream_sessions", []string{"OwnerID", "DeadlineKey", "key"}},
	{"records_query_business_dream_sessions_index_ownerid_assistantid_state", "business.dream_sessions_index", []string{"OwnerID", "AssistantID", "State"}},
	{"records_query_business_dream_sessions_index_state_deadlinekey", "business.dream_sessions_index", []string{"State", "DeadlineKey"}},
	{"records_query_business_groups_ownerid_position_key", "business.groups", []string{"OwnerID", "Position", "key"}},
	{"records_query_business_plan_links_handoffpending_key", "business.plan-links", []string{"HandoffPending", "key"}},
	{"records_query_business_plan_links_ownerid_ref_kind_ref_id_key", "business.plan-links", []string{"OwnerID", "Ref.Kind", "Ref.ID", "key"}},
	{"records_query_business_resources_kind_state_key", "business.resources", []string{"Kind", "State", "key"}},
	{"records_query_business_resources_ownerid_kind_key", "business.resources", []string{"OwnerID", "Kind", "key"}},
	{"records_query_business_resources_ownerid_provider_key", "business.resources", []string{"OwnerID", "Provider", "key"}},
	{"records_query_business_resources_sandboxid_key", "business.resources", []string{"SandboxID", "key"}},
	{"records_query_business_revisions_revisionscope_number_key", "business.revisions", []string{"RevisionScope", "Number", "key"}},
	{"records_query_business_watches_ownerid_assistantid_key", "business.watches", []string{"OwnerID", "AssistantID", "key"}},
	{"records_query_business_watches_ownerid_state_key", "business.watches", []string{"OwnerID", "State", "key"}},
	{"records_query_business_schedules_enabled_nextatkey", "business.schedules", []string{"Enabled", "NextAtKey"}},
	{"records_query_business_schedules_ownerid_assistantid_key", "business.schedules", []string{"OwnerID", "AssistantID", "key"}},
	{"records_query_business_tasks_ownerid_assistantid_key", "business.tasks", []string{"OwnerID", "AssistantID", "key"}},
	{"records_query_business_tasks_ownerid_assistantid_updatedatkey_key", "business.tasks", []string{"OwnerID", "AssistantID", "UpdatedAtKey", "key"}},
	{"records_query_agent_jobs_event_ownerid_event_taskid_state", "agent.jobs", []string{"Event.OwnerID", "Event.TaskID", "State"}},
	{"records_query_business_tasks_ownerid_plan_kind_plan_id", "business.tasks", []string{"OwnerID", "Plan.Kind", "Plan.ID"}},
	{"records_query_business_tasks_plan_id_key", "business.tasks", []string{"Plan.ID", "key"}},
	{"records_query_business_todos_ownerid_assistantid_key", "business.todos", []string{"OwnerID", "AssistantID", "key"}},
	{"records_query_context_counts_count_key", "context.counts", []string{"Count", "key"}},
	{"records_query_context_references_ownerid_assistantid_kind_key", "context.references", []string{"OwnerID", "AssistantID", "Kind", "key"}},
	{"records_query_debug_summaries_ownerid_assistantid_lastrevision_key", "debug.summaries", []string{"OwnerID", "AssistantID", "LastRevision", "key"}},
	{"records_query_debug_summaries_ownerid_assistantid_updatedrevision_key", "debug.summaries", []string{"OwnerID", "AssistantID", "UpdatedRevision", "key"}},
	{"records_query_debug_summaries_ownerid_id_key", "debug.summaries", []string{"OwnerID", "ID", "key"}},
	{"records_query_debug_summaries_ownerid_lastrevision_key", "debug.summaries", []string{"OwnerID", "LastRevision", "key"}},
	{"records_query_debug_summaries_ownerid_startedatkey_firstrevision_id_key", "debug.summaries", []string{"OwnerID", "StartedAtKey", "FirstRevision", "ID", "key"}},
	{"records_query_debug_summaries_ownerid_startedatkey_firstrevision_key", "debug.summaries", []string{"OwnerID", "StartedAtKey", "FirstRevision", "key"}},
	{"records_query_debug_summaries_ownerid_startedatkey_id_key", "debug.summaries", []string{"OwnerID", "StartedAtKey", "ID", "key"}},
	{"records_query_debug_summaries_ownerid_state_key", "debug.summaries", []string{"OwnerID", "State", "key"}},
	{"records_query_debug_summaries_ownerid_trigger_bytes", "debug.summaries", []string{"OwnerID", "Trigger", "Bytes"}},
	{"records_query_debug_summaries_ownerid_trigger_key", "debug.summaries", []string{"OwnerID", "Trigger", "key"}},
	{"records_query_debug_summaries_ownerid_updatedrevision_key", "debug.summaries", []string{"OwnerID", "UpdatedRevision", "key"}},
	{"records_query_debug_summaries_state_key", "debug.summaries", []string{"State", "key"}},
	{"records_query_dingtalk_bindings_clientid_key", "dingtalk.bindings", []string{"ClientID", "key"}},
	{"records_query_dingtalk_deliveries_bindingid_state_key", "dingtalk.deliveries", []string{"BindingID", "State", "key"}},
	{"records_query_dingtalk_deliveries_bindingid_state_sequence_part_key", "dingtalk.deliveries", []string{"BindingID", "State", "Sequence", "Part", "key"}},
	{"records_query_dingtalk_deliveries_state_key", "dingtalk.deliveries", []string{"State", "key"}},
	{"records_query_feishu_bindings_appid_key", "feishu.bindings", []string{"AppID", "key"}},
	{"records_query_feishu_bindings_state_key", "feishu.bindings", []string{"State", "key"}},
	{"records_query_feishu_deliveries_bindingid_state_sequence_part_key", "feishu.deliveries", []string{"BindingID", "State", "Sequence", "Part", "key"}},
	{"records_query_feishu_deliveries_state_sequence_part_key", "feishu.deliveries", []string{"State", "Sequence", "Part", "key"}},
	{"records_query_im_conversations_ownerid_peerid_key", "im.conversations", []string{"OwnerID", "PeerID", "key"}},
	{"records_query_im_messages_ownerkey_conversationid_createdatkey_key", "im.messages", []string{"OwnerKey", "ConversationID", "CreatedAtKey", "key"}},
	{"records_query_im_messages_ownerkey_conversationid_sender_sequence_key", "im.messages", []string{"OwnerKey", "ConversationID", "Sender", "Sequence", "key"}},
	{"records_query_im_messages_ownerkey_conversationid_sequence", "im.messages", []string{"OwnerKey", "ConversationID", "Sequence"}},
	{"records_query_im_outbox_atkey_id_key", "im.outbox", []string{"AtKey", "ID", "key"}},
	{"records_query_im_outbox_id_key", "im.outbox", []string{"ID", "key"}},
	{"records_query_im_outbox_ownerid_conversationid_key", "im.outbox", []string{"OwnerID", "ConversationID", "key"}},
	{"records_query_im_receipts_ownerkey_message_sender", "im.receipts", []string{"OwnerKey", "Message.Sender"}},
	{"records_query_im_visible_ownerid_sequence", "im.visible", []string{"OwnerID", "Sequence"}},
	{"records_query_im_visible_ownerid_topic_sequence", "im.visible", []string{"OwnerID", "Topic", "Sequence"}},
	{"records_query_mcp_calls_connection_id_createdat_key", "mcp.calls", []string{"Connection.ID", "CreatedAt", "key"}},
	{"records_query_mcp_calls_connection_id_createdatkey_key", "mcp.calls", []string{"Connection.ID", "CreatedAtKey", "key"}},
	{"records_query_mcp_calls_connection_id_createdatkey_state_ownerid_id", "mcp.calls", []string{"Connection.ID", "CreatedAtKey", "State", "OwnerID", "ID"}},
	{"records_query_mcp_calls_connection_id_ownerid_createdat", "mcp.calls", []string{"Connection.ID", "OwnerID", "CreatedAt"}},
	{"records_query_mcp_calls_connection_id_ownerid_createdatkey_state_id", "mcp.calls", []string{"Connection.ID", "OwnerID", "CreatedAtKey", "State", "ID"}},
	{"records_query_mcp_calls_connection_id_ownerid_state", "mcp.calls", []string{"Connection.ID", "OwnerID", "State"}},
	{"records_query_mcp_calls_connection_id_ownerid_state_createdatkey_id", "mcp.calls", []string{"Connection.ID", "OwnerID", "State", "CreatedAtKey", "ID"}},
	{"records_query_mcp_calls_connection_id_state_ownerid_createdatkey_id", "mcp.calls", []string{"Connection.ID", "State", "OwnerID", "CreatedAtKey", "ID"}},
	{"records_query_mcp_calls_ownerid_state_connection_id_createdatkey_id", "mcp.calls", []string{"OwnerID", "State", "Connection.ID", "CreatedAtKey", "ID"}},
	{"records_query_mcp_calls_state_key", "mcp.calls", []string{"State", "key"}},
	{"records_query_notifications_records_kind_resolvedat", "notifications.records", []string{"Kind", "ResolvedAt"}},
	{"records_query_notifications_records_ownerid_createdatkey_desc_id_desc_key_desc", "notifications.records", []string{"OwnerID", "CreatedAtKey DESC", "ID DESC", "key DESC"}},
	{"records_query_notifications_records_ownerid_resolvedat_dismissedat_createdatkey_desc_id_desc_key_desc", "notifications.records", []string{"OwnerID", "ResolvedAt", "DismissedAt", "CreatedAtKey DESC", "ID DESC", "key DESC"}},
	{"records_query_notifications_records_ownerid_resolvedat_dismissedat_readat_createdatkey_desc_id_desc_key_desc", "notifications.records", []string{"OwnerID", "ResolvedAt", "DismissedAt", "ReadAt", "CreatedAtKey DESC", "ID DESC", "key DESC"}},
	{"records_query_plugins_connections_ownerid_pluginid_deleted_key", "plugins.connections", []string{"OwnerID", "PluginID", "Deleted", "key"}},
	{"records_query_plugins_connections_ownerid_pluginid_key", "plugins.connections", []string{"OwnerID", "PluginID", "key"}},
	{"records_query_plugins_http_calls_connection_id_createdat_key", "plugins.http-calls", []string{"Connection.ID", "CreatedAt", "key"}},
	{"records_query_plugins_http_calls_connection_id_createdatkey_key", "plugins.http-calls", []string{"Connection.ID", "CreatedAtKey", "key"}},
	{"records_query_plugins_http_calls_connection_id_createdatkey_state_ownerid_id", "plugins.http-calls", []string{"Connection.ID", "CreatedAtKey", "State", "OwnerID", "ID"}},
	{"records_query_plugins_http_calls_connection_id_ownerid_createdat", "plugins.http-calls", []string{"Connection.ID", "OwnerID", "CreatedAt"}},
	{"records_query_plugins_http_calls_connection_id_ownerid_createdatkey_state_id", "plugins.http-calls", []string{"Connection.ID", "OwnerID", "CreatedAtKey", "State", "ID"}},
	{"records_query_plugins_http_calls_connection_id_ownerid_state", "plugins.http-calls", []string{"Connection.ID", "OwnerID", "State"}},
	{"records_query_plugins_http_calls_connection_id_ownerid_state_createdatkey_id", "plugins.http-calls", []string{"Connection.ID", "OwnerID", "State", "CreatedAtKey", "ID"}},
	{"records_query_plugins_http_calls_connection_id_state_ownerid_createdatkey_id", "plugins.http-calls", []string{"Connection.ID", "State", "OwnerID", "CreatedAtKey", "ID"}},
	{"records_query_plugins_http_calls_ownerid_state_connection_id_createdatkey_id", "plugins.http-calls", []string{"OwnerID", "State", "Connection.ID", "CreatedAtKey", "ID"}},
	{"records_query_plugins_http_calls_state_key", "plugins.http-calls", []string{"State", "key"}},
	{"records_query_plugins_installations_ownerid_deleted_key", "plugins.installations", []string{"OwnerID", "Deleted", "key"}},
	{"records_query_qq_bindings_appid_key", "qq.bindings", []string{"AppID", "key"}},
	{"records_query_qq_bindings_state_key", "qq.bindings", []string{"State", "key"}},
	{"records_query_qq_deliveries_bindingid_state_sequence_part_key", "qq.deliveries", []string{"BindingID", "State", "Sequence", "Part", "key"}},
	{"records_query_qq_deliveries_state_sequence_part_key", "qq.deliveries", []string{"State", "Sequence", "Part", "key"}},
	{"records_query_sandbox_connections_ownerid_view_endpoint_key", "sandbox.connections", []string{"OwnerID", "View.Endpoint", "key"}},
	{"records_query_sandbox_operations_reply_state_key", "sandbox.operations", []string{"Reply.State", "key"}},
	{"records_query_sandbox_shell_tasks_ownerid_assistantid_key", "sandbox.shell_tasks", []string{"OwnerID", "AssistantID", "key"}},
	{"records_query_sandbox_shell_tasks_ownerid_assistantid_startedatkey_key", "sandbox.shell_tasks", []string{"OwnerID", "AssistantID", "StartedAtKey", "key"}},
	{"records_query_sandbox_shell_tasks_ownerid_assistantid_state_startedatkey_key", "sandbox.shell_tasks", []string{"OwnerID", "AssistantID", "State", "StartedAtKey", "key"}},
	{"records_query_sandbox_shell_tasks_state_key", "sandbox.shell_tasks", []string{"State", "key"}},
	{"records_query_security_audit_atkey_id_key", "security.audit", []string{"AtKey", "ID", "key"}},
	{"records_query_security_audit_ownerid_atkey_desc_id_key_desc", "security.audit", []string{"OwnerID", "AtKey DESC", "ID", "key DESC"}},
	{"records_query_security_audit_ownerid_atkey_id_key", "security.audit", []string{"OwnerID", "AtKey", "ID", "key"}},
	{"records_query_security_credentials_metadata_ownerid_key", "security.credentials", []string{"Metadata.OwnerID", "key"}},
	{"records_query_security_grants_ownerid_assistantid_key", "security.grants", []string{"OwnerID", "AssistantID", "key"}},
	{"records_query_security_grants_ownerid_assistantid_resourceid_key", "security.grants", []string{"OwnerID", "AssistantID", "ResourceID", "key"}},
	{"records_query_security_grants_ownerid_assistantid_resourceid_revokedat_expiresatkey_key", "security.grants", []string{"OwnerID", "AssistantID", "ResourceID", "RevokedAt", "ExpiresAtKey", "key"}},
	{"records_query_security_grants_ownerid_assistantid_revokedat_expiresatkey_key", "security.grants", []string{"OwnerID", "AssistantID", "RevokedAt", "ExpiresAtKey", "key"}},
	{"records_query_security_permission_requests_ownerid_assistantid_createdatkey_desc_id_key_desc_state", "security.permission_requests", []string{"OwnerID", "AssistantID", "CreatedAtKey DESC", "ID", "key DESC", "State"}},
	{"records_query_security_permission_requests_ownerid_assistantid_key", "security.permission_requests", []string{"OwnerID", "AssistantID", "key"}},
	{"records_query_security_permission_requests_ownerid_assistantid_state_createdatkey_desc_id_key_desc", "security.permission_requests", []string{"OwnerID", "AssistantID", "State", "CreatedAtKey DESC", "ID", "key DESC"}},
	{"records_query_security_reservations_ownerid_assistantid_createdat", "security.reservations", []string{"OwnerID", "AssistantID", "CreatedAt"}},
	{"records_query_security_reservations_ownerid_assistantid_reservation_state", "security.reservations", []string{"OwnerID", "AssistantID", "Reservation.State"}},
	{"records_query_security_reservations_ownerid_createdat", "security.reservations", []string{"OwnerID", "CreatedAt"}},
	{"records_query_security_reservations_ownerid_modelid_createdat", "security.reservations", []string{"OwnerID", "ModelID", "CreatedAt"}},
	{"records_query_security_reservations_ownerid_modelid_reservation_state", "security.reservations", []string{"OwnerID", "ModelID", "Reservation.State"}},
	{"records_query_security_reservations_ownerid_reservation_operationid", "security.reservations", []string{"OwnerID", "Reservation.OperationID"}},
	{"records_query_slack_bindings_appid_key", "slack.bindings", []string{"AppID", "key"}},
	{"records_query_slack_bindings_teamid_userid_key", "slack.bindings", []string{"TeamID", "UserID", "key"}},
	{"records_query_slack_deliveries_bindingid_state_key", "slack.deliveries", []string{"BindingID", "State", "key"}},
	{"records_query_slack_deliveries_bindingid_state_sequence_part_key", "slack.deliveries", []string{"BindingID", "State", "Sequence", "Part", "key"}},
	{"records_query_slack_deliveries_ownerkey_bindingid_createdatkey_key", "slack.deliveries", []string{"OwnerKey", "BindingID", "CreatedAtKey", "key"}},
	{"records_query_slack_deliveries_state_key", "slack.deliveries", []string{"State", "key"}},
	{"records_query_slack_incoming_bindingid_state_createdatkey_key", "slack.incoming", []string{"BindingID", "State", "CreatedAtKey", "key"}},
	{"records_query_storage_files_state_key", "storage.files", []string{"State", "key"}},
	{"records_query_storage_files_state_size", "storage.files", []string{"State", "Size"}},
	{"records_query_telegram_bindings_botid_key", "telegram.bindings", []string{"BotID", "key"}},
	{"records_query_telegram_bindings_state_key", "telegram.bindings", []string{"State", "key"}},
	{"records_query_telegram_deliveries_bindingid_state_key", "telegram.deliveries", []string{"BindingID", "State", "key"}},
	{"records_query_telegram_deliveries_bindingid_state_sequence_part_key", "telegram.deliveries", []string{"BindingID", "State", "Sequence", "Part", "key"}},
	{"records_query_telegram_deliveries_ownerkey_bindingid_createdatkey_key", "telegram.deliveries", []string{"OwnerKey", "BindingID", "CreatedAtKey", "key"}},
	{"records_query_telegram_deliveries_state_key", "telegram.deliveries", []string{"State", "key"}},
	{"records_query_weixin_bindings_botid_key", "weixin.bindings", []string{"BotID", "key"}},
	{"records_query_weixin_bindings_state_key", "weixin.bindings", []string{"State", "key"}},
	{"records_query_weixin_bindings_userid_key", "weixin.bindings", []string{"UserID", "key"}},
	{"records_query_weixin_deliveries_bindingid_state_key", "weixin.deliveries", []string{"BindingID", "State", "key"}},
	{"records_query_weixin_deliveries_bindingid_state_sequence_part_key", "weixin.deliveries", []string{"BindingID", "State", "Sequence", "Part", "key"}},
	{"records_query_weixin_deliveries_ownerkey_bindingid_createdatkey_key", "weixin.deliveries", []string{"OwnerKey", "BindingID", "CreatedAtKey", "key"}},
	{"records_query_weixin_deliveries_state_key", "weixin.deliveries", []string{"State", "key"}},
}

func (s *Store) createIndexes() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	wanted := make(map[string]bool, len(recordIndexes))
	for _, index := range recordIndexes {
		wanted[index.name] = true
		parts := []string{"bucket"}
		for _, field := range index.fields {
			name, desc := strings.CutSuffix(field, " DESC")
			expr, err := fieldSQL(name)
			if desc {
				expr += " DESC"
			}
			if err != nil {
				return err
			}
			parts = append(parts, expr)
		}
		where := ""
		if index.bucket != "" {
			where = " WHERE bucket='" + index.bucket + "'"
		}
		if _, err := tx.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON records (%s)%s", index.name, strings.Join(parts, ","), where)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS auth_device_session ON records (json_extract(value, '$.Session.AccountID'), json_extract(value, '$.DeviceDigest')) WHERE bucket='auth.sessions' AND json_extract(value, '$.DeviceDigest') != ''`); err != nil {
		return err
	}
	// Context usage reads graph edges and byte counts, never message payloads.
	// Cover the exact projection expressions so SQLite can answer from metadata.
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS context_object_usage ON records(bucket,key,length(CAST(value AS BLOB)),value -> '$.Kind',value -> '$.Parent',value -> '$.Trees',value -> '$.Values') WHERE bucket='context.objects'`,
		`CREATE INDEX IF NOT EXISTS context_global_usage ON records(bucket,json_extract(value, '$.Kind'),length(CAST(value AS BLOB))) WHERE bucket='context.objects'`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	rows, err := tx.Query("SELECT name FROM sqlite_schema WHERE type='index' AND name GLOB 'records_query_*'")
	if err != nil {
		return err
	}
	var obsolete []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		if !wanted[name] {
			obsolete = append(obsolete, name)
		}
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	for _, name := range obsolete {
		if _, err := tx.Exec(`DROP INDEX "` + strings.ReplaceAll(name, `"`, `""`) + `"`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
