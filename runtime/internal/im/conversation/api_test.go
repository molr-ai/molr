package conversation

import (
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
)

func TestPublishingCannotImpersonateUserOrAnotherOperation(t *testing.T) {
	request := PublishRequest{Mutation: core.Mutation{IdempotencyKey: "publish-1"}, ConversationID: "chat", Content: Content{Kind: Text, Text: "hello"}}
	user := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "owner", Kind: core.ActorOwner, SessionID: "session"}, AssistantID: "assistant", OperationID: "op"}
	if request.Validate(user) == nil {
		t.Fatal("user principal accepted for assistant publication")
	}
	assistant := core.Execution{Principal: core.Principal{OwnerID: "owner", SubjectID: "assistant", Kind: core.ActorAssistant, ExecutionID: "run"}, AssistantID: "assistant", OperationID: "op"}
	if err := request.Validate(assistant); err != nil {
		t.Fatal(err)
	}
	request.Source.OperationID = "other-op"
	if request.Validate(assistant) == nil {
		t.Fatal("foreign operation accepted")
	}
}
func TestMessageContentEnforcesFixedCardAndSizeBoundaries(t *testing.T) {
	cases := []Content{{Kind: Text}, {Kind: Text, Text: strings.Repeat("x", 65537)}, {Kind: Card, Card: &CardContent{Kind: "html", TargetID: "x"}}, {Kind: Text, Text: "hello", Card: &CardContent{Kind: TaskCard, TargetID: "task"}}}
	for _, c := range cases {
		if c.Validate() == nil {
			t.Fatalf("invalid content accepted: %+v", c)
		}
	}
	card := Content{Kind: Card, Card: &CardContent{Kind: TaskCard, TargetID: "task"}}
	if err := card.Validate(); err != nil {
		t.Fatal(err)
	}
	user := SendUserRequest{Mutation: core.Mutation{IdempotencyKey: "send"}, ConversationID: "chat", Content: card}
	if user.Validate() == nil {
		t.Fatal("user spoofed a product card")
	}
}
