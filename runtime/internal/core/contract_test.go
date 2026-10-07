package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestTDDCoreIDBoundary(t *testing.T) {
	for _, id := range []ID{"a", "A_z-09", ID(strings.Repeat("x", 128))} {
		if err := id.Validate(); err != nil {
			t.Errorf("valid ID %q: %v", id, err)
		}
	}
	for _, id := range []ID{"", "../secret", "x/y", "x\\y", "a b", "中文", "\x00", ID(strings.Repeat("x", 129))} {
		if CodeOf(id.Validate()) != InvalidArgument {
			t.Errorf("unsafe ID %q accepted", id)
		}
	}
}

func TestTDDCorePrincipalAndExecutionBoundaries(t *testing.T) {
	owner := Principal{OwnerID: "owner", SubjectID: "owner", Kind: ActorOwner, TokenID: "token"}
	if err := owner.RequireOwner(); err != nil {
		t.Fatal(err)
	}
	assistant := Principal{OwnerID: "owner", SubjectID: "assistant", Kind: ActorAssistant, ExecutionID: "run"}
	if err := assistant.Validate(); err != nil {
		t.Fatal(err)
	}
	if CodeOf(assistant.RequireOwner()) != Forbidden {
		t.Fatal("assistant gained owner capability")
	}
	bad := []Principal{
		{}, {OwnerID: "owner", SubjectID: "owner", Kind: ActorOwner},
		{OwnerID: "owner", SubjectID: "other", Kind: ActorOwner, SessionID: "s"},
		{OwnerID: "owner", SubjectID: "owner", Kind: ActorOwner, SessionID: "s", TokenID: "t"},
		{OwnerID: "owner", SubjectID: "owner", Kind: ActorOwner, SessionID: "s", ExecutionID: "run"},
		{OwnerID: "owner", SubjectID: "assistant", Kind: ActorAssistant, SessionID: "s", ExecutionID: "run"},
		{OwnerID: "owner", SubjectID: "assistant", Kind: ActorAssistant, TokenID: "t", ExecutionID: "run"},
		{OwnerID: "owner", SubjectID: "assistant", Kind: ActorAssistant, ExecutionID: "../run"},
		{OwnerID: "owner", SubjectID: "owner", Kind: ActorKind("admin"), SessionID: "s"},
	}
	for i, p := range bad {
		if p.Validate() == nil || p.RequireOwner() == nil {
			t.Errorf("case %d accepted: %+v", i, p)
		}
	}
	execution := Execution{Principal: assistant, AssistantID: "assistant", TaskID: "task", OperationID: "operation"}
	if err := execution.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Execution){
		"wrong assistant":   func(e *Execution) { e.AssistantID = "other" },
		"owner execution":   func(e *Execution) { e.Principal = owner },
		"missing operation": func(e *Execution) { e.OperationID = "" },
		"bad task":          func(e *Execution) { e.TaskID = "../task" },
	} {
		t.Run(name, func(t *testing.T) {
			e := execution
			change(&e)
			if e.Validate() == nil {
				t.Fatal("invalid execution accepted")
			}
		})
	}
}

func TestTDDCoreMutationAndPagination(t *testing.T) {
	for _, key := range []string{"request-1", strings.Repeat("x", 128)} {
		if err := (Mutation{IdempotencyKey: key}).Validate(false); err != nil {
			t.Fatal(err)
		}
		if err := (Mutation{IdempotencyKey: key, ExpectedVersion: 1}).Validate(true); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"", " x", "x ", "a b", "x\n", strings.Repeat("x", 129), "中文"} {
		if (Mutation{IdempotencyKey: key, ExpectedVersion: 1}).Validate(false) == nil {
			t.Errorf("invalid key %q", key)
		}
	}
	if (Mutation{IdempotencyKey: "update"}).Validate(true) == nil {
		t.Fatal("versionless update accepted")
	}
	for _, limit := range []int{0, 1, 200} {
		p, err := (PageRequest{Limit: limit, Cursor: "opaque"}).Normalize()
		if err != nil {
			t.Fatal(err)
		}
		want := limit
		if want == 0 {
			want = 50
		}
		if p.Limit != want || p.Cursor != "opaque" {
			t.Fatalf("normalization changed cursor/bounds: %+v", p)
		}
	}
	for _, p := range []PageRequest{{Limit: -1}, {Limit: 201}, {Cursor: strings.Repeat("x", 2049)}} {
		if _, err := p.Normalize(); CodeOf(err) != InvalidArgument {
			t.Fatal("invalid page accepted", p)
		}
	}
}

func TestTDDCoreMoneyPrecisionAndCurrency(t *testing.T) {
	for _, micros := range []int64{0, 1, math.MaxInt64} {
		m := Money{Currency: "USD", Micros: micros}
		if err := m.Validate(); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Money
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded != m {
			t.Fatal("integer amount lost precision")
		}
	}
	for _, m := range []Money{{Currency: "usd"}, {Currency: "US"}, {Currency: "USDD"}, {Currency: "U$D"}, {Currency: "USD", Micros: -1}} {
		if CodeOf(m.Validate()) != InvalidArgument {
			t.Fatal("invalid money accepted", m)
		}
	}
}

func TestTDDCoreSecretEveryRepresentationIsRedacted(t *testing.T) {
	var empty Secret
	if !empty.Empty() || empty.Reveal() != "" {
		t.Fatal("zero secret is not empty")
	}
	secret := NewSecret("private-token-material")
	if secret.Empty() || secret.Reveal() != "private-token-material" {
		t.Fatal("trusted secret access changed")
	}
	jsonValue, err := secret.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	textValue, err := secret.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	outputs := []string{secret.String(), secret.GoString(), string(jsonValue), string(textValue)}
	for _, format := range []string{"%s", "%v", "%+v", "%#v", "%q", "%x", "%20s"} {
		outputs = append(outputs, fmt.Sprintf(format, secret))
	}
	for _, output := range outputs {
		if strings.Contains(output, secret.Reveal()) || !strings.Contains(output, "REDACTED") {
			t.Fatalf("unsafe representation: %q", output)
		}
	}
}

func TestTDDCoreErrorClassificationAndPrivateCause(t *testing.T) {
	cause := errors.New("private database pathname")
	wrapped := Wrap(Unavailable, "service unavailable", cause)
	if wrapped.Unwrap() != cause || !errors.Is(wrapped, cause) {
		t.Fatal("cause lost")
	}
	if strings.Contains(wrapped.Error(), cause.Error()) {
		t.Fatal("private cause exposed")
	}
	for _, code := range []Code{InvalidArgument, Unauthenticated, Forbidden, NotFound, Conflict, FailedPrecondition, RateLimited, BudgetExceeded, Unavailable, Canceled, DeadlineExceeded, Internal} {
		typed := NewError(code, "safe")
		if typed.Code != code || CodeOf(fmt.Errorf("outer: %w", typed)) != code {
			t.Fatalf("lost code %s", code)
		}
	}
	invalid := Invalid("field", "invalid input")
	if invalid.Field != "field" || invalid.Code != InvalidArgument {
		t.Fatal(invalid)
	}
	if CodeOf(nil) != "" || CodeOf(cause) != Internal || CodeOf(context.Canceled) != Canceled || CodeOf(context.DeadlineExceeded) != DeadlineExceeded {
		t.Fatal("incorrect standard classification")
	}
}
