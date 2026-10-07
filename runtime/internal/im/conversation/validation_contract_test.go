package conversation

import (
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func TestTDDContentValidationBoundaries(t *testing.T) {
	twenty := make([]core.ID, 20)
	for i := range twenty {
		twenty[i] = "attachment"
	}
	for _, content := range []Content{
		{Kind: Text, Text: strings.Repeat("x", 65536)},
		{Kind: Text, AttachmentIDs: twenty},
		{Kind: Card, Card: &CardContent{Kind: ResourceCard, TargetID: "resource", Title: strings.Repeat("x", 256), Summary: strings.Repeat("s", 8192)}},
		{Kind: Card, Card: &CardContent{Kind: ScheduleCard, TargetID: "schedule"}},
	} {
		testkit.RequireNoError(t, content.Validate())
	}
	for _, tc := range []struct {
		name    string
		content Content
	}{
		{"blank text", Content{Kind: Text, Text: " \t\n"}},
		{"invalid attachment", Content{Kind: Text, AttachmentIDs: []core.ID{"../secret"}}},
		{"too many attachments", Content{Kind: Text, AttachmentIDs: append(twenty, "extra")}},
		{"missing card", Content{Kind: Card}},
		{"card and text", Content{Kind: Card, Text: "text", Card: &CardContent{Kind: TaskCard, TargetID: "task"}}},
		{"card title limit", Content{Kind: Card, Card: &CardContent{Kind: TaskCard, TargetID: "task", Title: strings.Repeat("x", 257)}}},
		{"card summary limit", Content{Kind: Card, Card: &CardContent{Kind: TaskCard, TargetID: "task", Summary: strings.Repeat("x", 8193)}}},
		{"unknown kind", Content{Kind: "script", Text: "bad"}},
	} {
		t.Run(tc.name, func(t *testing.T) { testkit.RequireCode(t, tc.content.Validate(), core.InvalidArgument) })
	}
}
func TestTDDSendAndPublishValidateMutationTargetsAndSources(t *testing.T) {
	send := SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "send"}, ConversationID: "chat", Content: Content{Kind: Text, Text: "hello"}}
	testkit.RequireNoError(t, send.Validate())
	missingKey := send
	missingKey.Mutation.IdempotencyKey = ""
	testkit.RequireCode(t, missingKey.Validate(), core.InvalidArgument)
	badTarget := send
	badTarget.ConversationID = "../chat"
	testkit.RequireCode(t, badTarget.Validate(), core.InvalidArgument)
	execution := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "operation"}
	publish := PublishRequest{Mutation: send.Mutation, ConversationID: send.ConversationID, Content: send.Content, Source: Source{OperationID: "operation", References: []core.Object{{Kind: "task", ID: "task"}}}}
	testkit.RequireNoError(t, publish.Validate(execution))
	invalid := publish
	invalid.Source.References = []core.Object{{Kind: "", ID: "task"}}
	testkit.RequireCode(t, invalid.Validate(execution), core.InvalidArgument)
	invalid = publish
	invalid.Mutation.IdempotencyKey = ""
	testkit.RequireCode(t, invalid.Validate(execution), core.InvalidArgument)
	invalid = publish
	invalid.ConversationID = ""
	testkit.RequireCode(t, invalid.Validate(execution), core.InvalidArgument)
}
