package agent

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/tests/testkit"
)

func TestExecutionStepLimitDefaultsAndCeiling(t *testing.T) {
	for _, tc := range []struct{ requested, want int }{{0, 999}, {-1, 999}, {12, 12}, {999, 999}, {1000, 999}} {
		e := NewRuntime(nil, Options{MaxSteps: tc.requested})
		if e.options.MaxSteps != tc.want {
			t.Fatalf("%d resolved to %d", tc.requested, e.options.MaxSteps)
		}
	}
}

func TestDefaultExecutionContinuesPastTwelveSteps(t *testing.T) {
	calls := 0
	f, _ := makeLoopFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !testkit.DrainRequestBody(t, r) {
			return
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls <= 13 {
			fmt.Fprintf(w, `{"id":"step-%d","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"read-%d","type":"function","function":{"name":"conversation_read","arguments":"{\"conversation_id\":\"chat\",\"limit\":1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`, calls, calls)
		} else {
			fmt.Fprint(w, `{"id":"done","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`)
		}
	}))
	out, err := f.Runtime.Handle(t.Context(), f.Turn)
	if err != nil || calls != 15 || out.Status.WaitingReason != "" {
		t.Fatalf("calls=%d outcome=%+v err=%v", calls, out.Status, err)
	}
}

func TestOutputLimitAppliesToRequestAndBudget(t *testing.T) {
	for _, limit := range []int{0, 8192, 65536} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			f, _ := makeLoopFixture(t, nil)
			e := f.Runtime.(*Engine)
			want := model.DefaultOutputLimit
			if limit > 0 {
				want = limit
				e.options.OutputLimit = func(context.Context, core.Principal, core.ID) (int, error) { return limit, nil }
			}
			estimated := 0
			e.options.Estimate = func(_ context.Context, _ core.ID, _ []model.Input, output int) (core.Money, error) {
				estimated = output
				return core.Money{Currency: "USD", Micros: 1000}, nil
			}
			if _, err := e.Handle(t.Context(), f.Turn); err != nil {
				t.Fatal(err)
			}
			reqs := f.ModelRequests()
			if len(reqs) != 2 || reqs[0].MaxOutputTokens != want || reqs[1].MaxOutputTokens != want || estimated != want {
				t.Fatalf("requests=%+v estimated=%d want=%d", reqs, estimated, want)
			}
		})
	}
}

func TestOutputLimitDoesNotDependOnContextEstimate(t *testing.T) {
	f, _ := makeLoopFixture(t, nil)
	e := f.Runtime.(*Engine)
	e.options.ContextWindow = func(context.Context, core.Principal, core.ID) (int, error) { return 8192, nil }
	p := turnProgress{Inputs: []model.Input{{Role: model.User, Text: "small context"}}}
	p.Usage = &contextUsage{ModelID: "model", Tokens: 1000000, Inputs: usageInputs(p.Inputs, model.StateSource{}), Tools: contextHash(p.Definitions)}
	if _, _, err := e.generateStep(t.Context(), f.Turn, "model", nil, p); err != nil {
		t.Fatal(err)
	}
	if got := f.ModelRequests()[0].MaxOutputTokens; got != model.DefaultOutputLimit {
		t.Fatalf("estimate changed output cap: %d", got)
	}
	e.options.OutputLimit = func(context.Context, core.Principal, core.ID) (int, error) { return 128, nil }
	if _, _, err := e.generateContextStep(t.Context(), f.Turn, "model", nil, p, "summary", 4096); err != nil {
		t.Fatal(err)
	}
	if got := f.ModelRequests()[1].MaxOutputTokens; got != 128 {
		t.Fatalf("summary exceeded model limit: %d", got)
	}
}
