package bridge

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/radosny/airdrive/internal/auth"
	"github.com/radosny/airdrive/internal/index"
	"github.com/radosny/airdrive/internal/provider"
	mockprovider "github.com/radosny/airdrive/internal/provider/mock"
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

func TestWhenTheLocationProviderIsHealthyThenHealthReportsConnected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)
	ctrl := gomock.NewController(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindS3, Name: "R2", Root: "content"})
	svc.registry.Register("L1", mockprovider.NewMockStorageProvider(ctrl))

	h, err := svc.LocationHealth("L1")
	r.NoError(err)
	r.True(h.Connected)
	r.False(h.NeedsAuth)
}

func TestWhenTheGdriveRefreshTokenIsRevokedThenHealthAsksForAuth(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"})
	wireGdrive(t, svc, &gdriveDeps{providerErr: errors.New(`oauth2: "invalid_grant" "Token has been expired or revoked."`)})

	h, err := svc.LocationHealth("L1")
	r.NoError(err)
	r.False(h.Connected)
	r.True(h.NeedsAuth)
	r.Contains(h.Error, "sign-in has expired")
}

func TestWhenTheProviderFailsForAnotherReasonThenHealthDoesNotAskForAuth(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	svc, db, _ := openService(t)

	seedLocation(t, db, index.Location{ID: "L1", Kind: provider.KindGDrive, Name: "Drive", Root: "My Drive"})
	wireGdrive(t, svc, &gdriveDeps{providerErr: errors.New("dial tcp: network is unreachable")})

	h, err := svc.LocationHealth("L1")
	r.NoError(err)
	r.False(h.Connected)
	r.False(h.NeedsAuth)
}
