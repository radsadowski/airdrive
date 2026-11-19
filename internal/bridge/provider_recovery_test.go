package bridge

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radosny/airdrive/internal/auth"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
)

func TestWhenRestoreFailsThenListLocationsKeepsReportingTheError(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"})
	wireGdrive(t, svc, &gdriveDeps{loadTokenErr: errors.New("keychain: not found")})

	r.NoError(svc.Restore())

	locs, err := svc.ListLocations()
	r.NoError(err)
	r.Len(locs, 1)
	r.Equal("error", locs[0].Status, "a failed restore must survive a reload of the location list")
	r.Contains(locs[0].Error, "Google Drive is not connected")
}

func TestWhenARestoreFailedThenTheRegistryRebuildsTheProviderOnFirstUse(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openService(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"})
	deps := wireGdrive(t, svc, &gdriveDeps{})
	syncScript(deps.provider, "CURSOR", nil, nil)
	svc.registry.SetLoader(svc.providerFor)

	p, err := svc.registry.Get("L1")
	r.NoError(err)
	r.Same(deps.provider, p)
	waitIdle(t, rec)
}

func TestWhenALocalfsLocationWasNeverRegisteredThenTheLoaderBuildsIt(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)

	seedLocation(t, db, index.Location{ID: "LOCAL1", Kind: provider.KindLocalFS, Name: "Local", Root: t.TempDir()})
	svc.registry.SetLoader(svc.providerFor)

	p, err := svc.registry.Get("LOCAL1")
	r.NoError(err)
	r.Equal(provider.KindLocalFS, p.Kind())
}

func TestWhenAGdriveLocationIsReconnectedThenTheSameLocationKeepsWorking(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, rec := openService(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"})
	deps := wireGdrive(t, svc, &gdriveDeps{signInToken: auth.GdriveToken{AccessToken: "NEW", RefreshToken: "RT2"}})
	syncScript(deps.provider, "CURSOR", nil, nil)

	loc, err := svc.ReconnectGdrive("L1")
	r.NoError(err)
	r.Equal("L1", loc.ID, "reconnect keeps the location id so sync pairs stay valid")
	r.Equal([]string{"L1"}, deps.saved, "the new token is stored against the existing location")

	p, err := svc.registry.Get("L1")
	r.NoError(err)
	r.Same(deps.provider, p)
	waitIdle(t, rec)
}

func TestWhenReconnectTargetsANonGdriveLocationThenItIsRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)

	seedLocation(t, db, index.Location{ID: "LOCAL1", Kind: provider.KindLocalFS, Name: "Local", Root: t.TempDir()})

	_, err := svc.ReconnectGdrive("LOCAL1")
	r.ErrorContains(err, "not gdrive")
}

func TestWhenTheGdriveTokenWasRevokedThenTheHintAsksForAReconnect(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	msg := connectHint(provider.KindGDrive, errors.New(`oauth2: "invalid_grant" "Token has been expired or revoked."`))

	r.Contains(msg, "sign-in has expired")
	r.Contains(msg, "Reconnect the location")
	r.NotContains(msg, "invalid_grant", "the raw OAuth error belongs in the log, not the sidebar")
}
