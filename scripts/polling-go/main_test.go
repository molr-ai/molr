package main

import "testing"

func TestTimerForms(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		apis         []string
	}{
		{"ticker", `package p;import "time";func run(){ t:=time.NewTicker(100*time.Millisecond);for range t.C{} }`, []string{"time.NewTicker"}},
		{"renamed import and loop", `package p;import clock "time";func run(){for{clock.Sleep(100*clock.Millisecond)}}`, []string{"time.Sleep"}},
		{"dot import", `package p;import . "time";func run(){for{<-After(Second)}}`, []string{"time.After"}},
		{"timer reset", `package p;import "time";func run(){ t:=time.NewTimer(time.Hour); alias:=t;for{alias.Reset(100*time.Millisecond)} }`, []string{"time.NewTimer", "timer.Reset"}},
		{"function alias", `package p;import "time";var later=time.After;func run(){<-later(time.Second)}`, []string{"reference:time.After", "time.After"}},
		{"ordinary methods", `package p;func run(){ text.Reset(); other.After(10) }`, nil},
		{"callback", `package p;import "time";func run(){time.AfterFunc(time.Second,func(){run()})}`, []string{"time.AfterFunc"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := scanSource("fixture.go", tc.source)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != len(tc.apis) {
				t.Fatalf("%+v", rows)
			}
			for i, r := range rows {
				if r.API != tc.apis[i] {
					t.Fatalf("%+v", rows)
				}
			}
		})
	}
}
func TestDelayAndLoopChangesRemainVisible(t *testing.T) {
	a, _ := scanSource("fixture.go", `package p;import "time";func run(){<-time.After(time.Second)}`)
	b, _ := scanSource("fixture.go", `package p;import "time";func run(){for{<-time.After(100*time.Millisecond)}}`)
	if a[0].Delay == b[0].Delay || a[0].Loops == b[0].Loops {
		t.Fatal(a, b)
	}
}

func TestGlobalAndAliasedCadenceChangesRemainVisible(t *testing.T) {
	a, _ := scanSource("fixture.go", `package p;import "time";const base=time.Second;func run(){delay:=base;time.NewTicker(2*delay)}`)
	b, _ := scanSource("fixture.go", `package p;import "time";const base=100*time.Millisecond;func run(){delay:=base;time.NewTicker(2*delay)}`)
	if len(a) != 1 || len(b) != 1 || a[0].Delay == b[0].Delay {
		t.Fatal(a, b)
	}
}
func TestTypedTimerResetIsReviewed(t *testing.T) {
	rows, err := scanSource("fixture.go", `package p;import "time";func run(timer *time.Timer){timer.Reset(100*time.Millisecond)}`)
	if err != nil || len(rows) != 1 || rows[0].API != "timer.Reset" {
		t.Fatal(rows, err)
	}
}

func TestPollingConfigurationCannotBypassTimerReview(t *testing.T) {
	rows, err := scanSource("fixture.go", `package p;import "time";func run(){opts.TickInterval=100*time.Millisecond;start(Options{AuthorizationInterval:100*time.Millisecond})}`)
	if err != nil || len(rows) != 2 || rows[0].API != "policy.TickInterval" || rows[1].API != "policy.AuthorizationInterval" {
		t.Fatal(rows, err)
	}
}

func TestProductionConstructorsRequireEventCapabilities(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`r.NewFeed(store,r.Options{})`, "missing:WatchAuthorization"},
		{`r.NewFeed(store,r.Options{WatchAuthorization:nil})`, "missing:WatchAuthorization"},
		{`r.NewFeed(store,r.Options{WatchAuthorization:watch})`, "WatchAuthorization"},
		{`s.NewService(store,s.Options{TickSignals:signals})`, "missing:NextTick"},
		{`s.NewService(store,s.Options{TickSignals:signals,NextTick:deadline})`, "TickSignals+NextTick"},
	} {
		rows, err := scanSource("fixture.go", `package p;import r "molr.ai/runtime/internal/im/realtime";import s "molr.ai/runtime/internal/agent/scheduler";func run(){`+tc.body+`}`)
		if err != nil || len(rows) != 1 || rows[0].Delay != tc.want {
			t.Fatalf("%s: %+v %v", tc.body, rows, err)
		}
	}
}
