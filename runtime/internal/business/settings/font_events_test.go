package settings

import (
	"errors"
	"testing"

	"molr.ai/runtime/internal/im/realtime"
)

func TestFontSizeEventsCommitOnceAndRollbackWithSettings(t *testing.T) {
	db := fixtureDB(t)
	service := NewService(db, Options{Authorizer: fixtureAuthorizer{}})
	ctx := t.Context()
	current, err := service.Get(ctx, owner())
	if err != nil {
		t.Fatal(err)
	}
	stream := realtime.NewStore(db)
	watch, err := stream.Watch(owner().OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	current.FontSize = FontLarge
	request := UpdateRequest{Mutation: mutation("font-event", current.Version), Settings: current}
	failure := errors.New("commit failure")
	db.FailNextCommit(failure)
	if _, err = service.Update(ctx, owner(), request); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	select {
	case <-watch.Changes():
		t.Fatal("rollback woke subscriber")
	default:
	}
	events, err := stream.Replay(ctx, owner().OwnerID, realtime.Query{})
	if err != nil || len(events.Items) != 0 {
		t.Fatal("rollback published event", events, err)
	}
	saved, err := service.Get(ctx, owner())
	if err != nil || saved.FontSize != FontStandard {
		t.Fatal("rollback changed preference", saved, err)
	}
	if _, err = service.Update(ctx, owner(), request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-watch.Changes():
	default:
		t.Fatal("commit did not wake subscriber")
	}
	if _, err = service.Update(ctx, owner(), request); err != nil {
		t.Fatal(err)
	}
	events, err = stream.Replay(ctx, owner().OwnerID, realtime.Query{})
	if err != nil || len(events.Items) != 1 || events.Items[0].Topic != "settings.changed" || events.Items[0].Version != 1 {
		t.Fatal(events, err)
	}
	foreign, err := stream.Replay(ctx, otherOwner().OwnerID, realtime.Query{})
	if err != nil || len(foreign.Items) != 0 {
		t.Fatal("foreign event", foreign, err)
	}
}
