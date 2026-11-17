package league

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/store"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// A league's entries are numbered in commit order (ADR 0017), whoever
// writes them: an action holds the league's lock, and an entry written
// without loading the league first must wait for it rather than take a
// lower number than the entry that commits before it. Otherwise a member
// who marks the newer entry seen in between skips the older one for good.
func TestAuditSeqFollowsCommitOrder(t *testing.T) {
	pool := storetest.New(t)
	seedUsers(t, pool)
	repo := NewPostgres(pool)
	m := NewService(repo, time.Now)
	l, err := m.Create(ctx, usrA, "Ordered League", 4, "A", SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}

	// A holds the league's lock, as every action does.
	locked, release := make(chan struct{}), make(chan struct{})
	errs := make(chan error, 2)
	var first AuditEntry
	go func() {
		errs <- repo.Tx(ctx, func(tx Tx) error {
			lg, err := tx.Load(ctx, l.ID)
			if err != nil {
				return err
			}
			close(locked)
			<-release
			first = NewAudit(lg, usrA, AuditInviteCreated, "first to commit", nil, time.Now())
			return tx.AddAudit(ctx, &first)
		})
	}()
	<-locked

	// B writes an entry without loading the league.
	pid := make(chan int, 1)
	var second AuditEntry
	go func() {
		errs <- repo.TxWith(ctx, func(tx Tx, db store.DB) error {
			var p int
			if err := db.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&p); err != nil {
				return err
			}
			pid <- p
			second = NewAudit(l, usrA, AuditSignInLinkIssued, "second to commit", nil, time.Now())
			return tx.AddAudit(ctx, &second)
		})
	}()
	p := <-pid
	for deadline := time.Now().Add(10 * time.Second); ; {
		var waiting bool
		if err := pool.QueryRow(ctx,
			`SELECT coalesce((SELECT wait_event_type = 'Lock' FROM pg_stat_activity WHERE pid = $1), false)`, p).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("B never waited for the league's lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if first.Seq >= second.Seq {
		t.Fatalf("first committed has seq %d, the later one %d: a marker at %d would skip it", first.Seq, second.Seq, first.Seq)
	}
}
