package reaper

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/event"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/store"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/stream"
)

// Seeded sessions: idle started ten minutes ago with no events, recent
// started just now, and active started ten minutes ago but with an event
// received just now.
const (
	idleID   = "s-idle"
	recentID = "s-recent"
	activeID = "s-active"
)

// fixture is a Lifecycle over a clean, seeded test database with an
// overview subscription on the hub.
type fixture struct {
	lc       *session.Lifecycle
	overview <-chan stream.Message
}

// newFixture opens TEST_DATABASE_URL, migrates, truncates, subscribes and
// seeds through the lifecycle. It skips the test when TEST_DATABASE_URL is
// unset.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	if _, err := db.ExecContext(ctx, "TRUNCATE events, sessions CASCADE"); err != nil {
		t.Fatalf("truncate test db: %v", err)
	}
	hub := stream.NewHub()
	overview, cancel := hub.Subscribe(stream.Filter{})
	t.Cleanup(cancel)
	f := &fixture{lc: session.New(store.New(db), hub), overview: overview}
	f.seed(t)
	drain(f.overview) // seeding messages are not under test
	return f
}

func (f *fixture) seed(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	for _, s := range []session.StartedParams{
		{ID: idleID, SpaceID: "sp", StartedAt: now.Add(-10 * time.Minute), Seq: 1},
		{ID: recentID, SpaceID: "sp", StartedAt: now, Seq: 1},
		{ID: activeID, SpaceID: "sp", StartedAt: now.Add(-10 * time.Minute), Seq: 1},
	} {
		if _, _, err := f.lc.Started(ctx, s); err != nil {
			t.Fatalf("start %s: %v", s.ID, err)
		}
	}
	if _, err := f.lc.RecordEvents(ctx, activeID, "sp", "c1", now, []session.NewEvent{
		{Type: event.Click, Seq: 1, TS: now},
	}); err != nil {
		t.Fatalf("record event for %s: %v", activeID, err)
	}
}

// status returns the current status of a seeded session.
func (f *fixture) status(t *testing.T, id string) string {
	t.Helper()
	s, err := f.lc.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return s.Status
}

// waitEnded polls until the session is ended or the deadline passes.
func (f *fixture) waitEnded(t *testing.T, id string, deadline time.Duration) {
	t.Helper()
	stop := time.Now().Add(deadline)
	for f.status(t, id) != session.StatusEnded {
		if time.Now().After(stop) {
			t.Fatalf("%s still live after %v", id, deadline)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// start runs Run in a goroutine and returns the channel closed when it returns.
func start(ctx context.Context, lc *session.Lifecycle, idle, tick time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(ctx, lc, idle, tick)
	}()
	return done
}

func assertReturned(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of cancellation")
	}
}

func drain(ch <-chan stream.Message) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func TestRunReapsIdleOnly(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := start(ctx, f.lc, time.Minute, 50*time.Millisecond)
	f.waitEnded(t, idleID, 2*time.Second)
	for _, id := range []string{recentID, activeID} {
		if got := f.status(t, id); got != session.StatusLive {
			t.Errorf("%s status = %q, want live", id, got)
		}
	}

	// Exactly one hub message: the idle session, ended.
	select {
	case m := <-f.overview:
		p, ok := m.Data.(stream.SessionPayload)
		if m.Event != stream.EventSession || !ok {
			t.Fatalf("message %+v, want session", m)
		}
		if p.SessionID != idleID || p.Status != session.StatusEnded || p.EndedAt == nil {
			t.Errorf("payload %+v, want %s ended", p, idleID)
		}
	case <-time.After(time.Second):
		t.Fatal("no session message within 1s")
	}
	select {
	case m := <-f.overview:
		t.Fatalf("unexpected second message %+v", m)
	default:
	}

	cancel()
	assertReturned(t, done)
}

func TestRunSweepsBeforeFirstTick(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := start(ctx, f.lc, time.Minute, time.Hour)
	f.waitEnded(t, idleID, time.Second)

	cancel()
	assertReturned(t, done)
}

func TestRunReturnsWithoutSweepingOnCancelledContext(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assertReturned(t, start(ctx, f.lc, time.Minute, 50*time.Millisecond))
	if got := f.status(t, idleID); got != session.StatusLive {
		t.Errorf("%s status = %q, want live: cancelled Run swept", idleID, got)
	}
}
