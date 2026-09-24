package main

import (
	"path/filepath"
	"testing"
)

func shareTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "share.db"))
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestClaimNoLongerStealsAnInstallation is the defect this replaces. ClaimInstallation was an
// unconditional UPDATE of installations.account_id: a second account claiming the same id took
// it, and the previous owner's workers stayed connected while silently going blind — repos=[]
// with no error anywhere. On this fleet that would have knocked out four production workers.
func TestClaimNoLongerStealsAnInstallation(t *testing.T) {
	st := shareTestStore(t)
	st.UpsertInstallation(4242, "acme", []string{"acme/one", "acme/two"})

	if err := st.ClaimInstallation(4242, 1); err != nil {
		t.Fatalf("first claim should succeed: %v", err)
	}
	if err := st.ClaimInstallation(4242, 2); err == nil {
		t.Fatal("STOLEN: a second account claimed an installation it does not own")
	}

	// the original owner keeps its repos
	if got := st.EntitledRepos(1); !got["acme/one"] || !got["acme/two"] {
		t.Errorf("the owner lost entitlement: %v", got)
	}
	if got := st.EntitledRepos(2); len(got) != 0 {
		t.Errorf("the intruder should have no entitlement, got %v", got)
	}
}

// TestSharingGrantsWithoutRevoking: the case that made this necessary — a prod worker and a
// test worker on the same repos.
func TestSharingGrantsWithoutRevoking(t *testing.T) {
	st := shareTestStore(t)
	st.UpsertInstallation(4242, "acme", []string{"acme/one"})
	if err := st.ClaimInstallation(4242, 1); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := st.AddInstallationMember(4242, 2); err != nil {
		t.Fatalf("share: %v", err)
	}

	for _, acct := range []int64{1, 2} {
		if got := st.EntitledRepos(acct); !got["acme/one"] {
			t.Errorf("account %d should be entitled, got %v", acct, got)
		}
	}
	// and revoking one does not touch the other
	if err := st.RemoveInstallationMember(4242, 2); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got := st.EntitledRepos(2); len(got) != 0 {
		t.Errorf("revoked account should lose entitlement, got %v", got)
	}
	if got := st.EntitledRepos(1); !got["acme/one"] {
		t.Error("revoking a share must not affect the owner")
	}
}

// TestClaimIsIdempotent: re-running `mago link` must not error or duplicate.
func TestClaimIsIdempotent(t *testing.T) {
	st := shareTestStore(t)
	st.UpsertInstallation(7, "acme", []string{"acme/x"})
	for i := 0; i < 3; i++ {
		if err := st.ClaimInstallation(7, 1); err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
	}
	if ids := st.InstallationsFor(1); len(ids) != 1 {
		t.Errorf("expected one installation, got %v", ids)
	}
}

// TestUnknownInstallationStillErrors keeps the useful message for a typo'd id.
func TestUnknownInstallationStillErrors(t *testing.T) {
	st := shareTestStore(t)
	if err := st.ClaimInstallation(999, 1); err == nil {
		t.Error("claiming an installation that does not exist should fail")
	}
}

// TestBackfillKeepsExistingOwnerEntitled: upgrading must not strip access from an account that
// claimed its installation under the old single-owner model.
func TestBackfillKeepsExistingOwnerEntitled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "upgrade.db")

	st, err := openStore(path)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	st.UpsertInstallation(11, "acme", []string{"acme/legacy"})
	// simulate a pre-migration row: owner recorded only on installations.account_id
	st.db.Exec("UPDATE installations SET account_id=5 WHERE installation_id=11")
	st.db.Exec("DELETE FROM installation_members WHERE installation_id=11")
	st.Close()

	st2, err := openStore(path) // reopening runs the migration
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	if got := st2.EntitledRepos(5); !got["acme/legacy"] {
		t.Errorf("the pre-existing owner lost entitlement on upgrade: %v", got)
	}
}

// TestSharedInstallationIsListed guards an inconsistency found in live testing: entitlement
// moved to the membership table but the LISTING still read installations.account_id, so a
// shared account received events for repos while `mago link` told it nothing was linked.
func TestSharedInstallationIsListed(t *testing.T) {
	st := shareTestStore(t)
	st.UpsertInstallation(4242, "acme", []string{"acme/one"})
	if err := st.ClaimInstallation(4242, 1); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := st.AddInstallationMember(4242, 2); err != nil {
		t.Fatalf("share: %v", err)
	}

	for _, acct := range []int64{1, 2} {
		list := st.InstallationsForAccount(acct)
		if len(list) != 1 || list[0].ID != 4242 {
			t.Errorf("account %d should see the installation, got %+v", acct, list)
		}
		if got := st.EntitledRepos(acct); !got["acme/one"] {
			t.Errorf("account %d entitlement disagrees with its listing", acct)
		}
	}
	// and a revoked account sees neither
	st.RemoveInstallationMember(4242, 2)
	if list := st.InstallationsForAccount(2); len(list) != 0 {
		t.Errorf("revoked account should not list the installation, got %+v", list)
	}
}
