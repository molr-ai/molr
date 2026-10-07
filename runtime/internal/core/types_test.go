package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSecretCannotAccidentallyEscape(t *testing.T) {
	secret := NewSecret("live-secret-value")
	payload := struct{ Password Secret }{secret}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{fmt.Sprint(payload), fmt.Sprintf("%+v", payload), fmt.Sprintf("%#v", payload), fmt.Sprintf("%q", secret), string(encoded)} {
		if strings.Contains(output, "live-secret-value") {
			t.Fatalf("secret leaked: %s", output)
		}
	}
	if secret.Reveal() != "live-secret-value" {
		t.Fatal("trusted reveal failed")
	}
}
func TestPrincipalRejectsMixedAndForgedShapes(t *testing.T) {
	owner := Principal{OwnerID: "owner", SubjectID: "owner", Kind: ActorOwner, SessionID: "session"}
	assistant := Principal{OwnerID: "owner", SubjectID: "assistant", Kind: ActorAssistant, ExecutionID: "run"}
	for _, p := range []Principal{owner, assistant} {
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	invalid := []Principal{{}, {OwnerID: "owner", SubjectID: "other", Kind: ActorOwner, SessionID: "session"}, {OwnerID: "owner", SubjectID: "owner", Kind: ActorOwner, SessionID: "session", TokenID: "token"}, {OwnerID: "owner", SubjectID: "assistant", Kind: ActorAssistant}, {OwnerID: "owner", SubjectID: "assistant", Kind: ActorAssistant, ExecutionID: "run", TokenID: "token"}}
	for _, p := range invalid {
		if p.Validate() == nil {
			t.Fatalf("accepted invalid principal: %+v", p)
		}
	}
	if CodeOf(assistant.RequireOwner()) != Forbidden {
		t.Fatal("assistant received owner capability")
	}
}
func TestExecutionMustMatchAssistant(t *testing.T) {
	e := Execution{Principal: Principal{OwnerID: "o", SubjectID: "a", Kind: ActorAssistant, ExecutionID: "run"}, AssistantID: "different", OperationID: "op"}
	if e.Validate() == nil {
		t.Fatal("accepted wrong assistant")
	}
	e.AssistantID = "a"
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestMutationAndPaginationBounds(t *testing.T) {
	if (Mutation{IdempotencyKey: "send-1"}).Validate(true) == nil {
		t.Fatal("update without version")
	}
	if (Mutation{IdempotencyKey: "send\n1"}).Validate(false) == nil {
		t.Fatal("control character in idempotency key")
	}
	page, err := (PageRequest{}).Normalize()
	if err != nil || page.Limit != 50 {
		t.Fatal(page, err)
	}
	for _, page := range []PageRequest{{Limit: -1}, {Limit: 201}, {Cursor: strings.Repeat("x", 2049)}} {
		if _, err := page.Normalize(); err == nil {
			t.Fatal("unbounded page accepted")
		}
	}
}
func TestTypedErrorsPreservePrivateCause(t *testing.T) {
	cause := errors.New("private details")
	err := Wrap(Unavailable, "safe diagnostic", cause)
	if !errors.Is(err, cause) || CodeOf(fmt.Errorf("outer: %w", err)) != Unavailable {
		t.Fatal("error identity lost")
	}
	if strings.Contains(err.Error(), cause.Error()) {
		t.Fatal("private cause leaked")
	}
	if CodeOf(fmt.Errorf("wrapped: %w", context.Canceled)) != Canceled {
		t.Fatal("cancellation lost")
	}
}
