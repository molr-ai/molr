package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/core"
)

func TestPublicErrorsDoNotLeakDiagnostics(t *testing.T) {
	secret := "/private/config token=secret"
	for _, err := range []error{errors.New(secret), core.Wrap(core.Internal, secret, errors.New(secret)), core.Invalid(secret, secret)} {
		_, response := PublicError(err, "request")
		body, _ := json.Marshal(response)
		if strings.Contains(string(body), "secret") || strings.Contains(string(body), "private") {
			t.Fatalf("leaked: %s", body)
		}
	}
	status, response := PublicError(core.NewError(core.Code("unknown_secret"), secret), "invalid/id")
	if status != http.StatusInternalServerError || response.Code != core.Internal || response.RequestID != "" {
		t.Fatal(status, response)
	}
}

type nilAuth struct{ auth.Service }

func TestCompositionRejectsTypedNil(t *testing.T) {
	var missing *nilAuth
	services := Services{Auth: missing}
	err := services.Validate()
	var detail *core.Error
	if !errors.As(err, &detail) || detail.Field != "services.Auth" {
		t.Fatal("typed nil not rejected", err)
	}
}
func TestHTTPCompositionExcludesPrivilegedExecutionPorts(t *testing.T) {
	value := reflect.TypeOf(Services{})
	cases := map[string][]string{"Plans": {"Tick", "Migrate", "BindTask", "StartScheduled"}, "Conversations": {"Publish", "AcceptUserMessage"}, "Agent": {"Handle"}, "Models": {"Generate", "Reconcile"}, "Index": {"Apply"}, "Jobs": {"Run", "Recover"}, "Credentials": {"WithSecret"}, "Auth": {"Initialize", "Recover"}, "Budget": {"Reserve", "Settle", "MarkUnknown", "Release"}, "Operations": {"Begin", "Complete", "MarkUnknown"}}
	for field, names := range cases {
		f, ok := value.FieldByName(field)
		if !ok {
			t.Fatal(field)
		}
		for _, name := range names {
			if _, exists := f.Type.MethodByName(name); exists {
				t.Fatalf("HTTP received internal capability %s.%s", field, name)
			}
		}
	}
}

func TestPublicErrorReasonsAreReviewedAndKeepDiagnosticsPrivate(t *testing.T) {
	for _, reason := range []core.Reason{core.NetworkPrivateAddressDenied, core.NetworkHTTPNotAllowed, core.NetworkDestinationProhibited, "private-secret"} {
		err := core.WithReason(core.Invalid("private-secret", "private-secret"), reason)
		status, response := PublicError(err, "request")
		if status != http.StatusBadRequest || response.Code != core.InvalidArgument {
			t.Fatal(status, response)
		}
		if reason == "private-secret" {
			if response.Reason != "" {
				t.Fatal("unreviewed reason exposed")
			}
		} else if response.Reason != reason {
			t.Fatal(response)
		}
		body, _ := json.Marshal(response)
		if strings.Contains(string(body), "private-secret") {
			t.Fatal("diagnostics exposed", string(body))
		}
	}
}
