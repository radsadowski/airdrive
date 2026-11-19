package provider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/radosny/airdrive/internal/provider"
	mockprovider "github.com/radosny/airdrive/internal/provider/mock"
)

func TestWhenGetMissesThenTheLoaderBuildsAndRegistersTheProvider(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctrl := gomock.NewController(t)
	p := mockprovider.NewMockStorageProvider(ctrl)

	reg := provider.NewRegistry()
	var calls int
	reg.SetLoader(func(_ context.Context, locationID string) (provider.StorageProvider, error) {
		calls++
		r.Equal("L1", locationID)
		return p, nil
	})

	got, err := reg.Get("L1")
	r.NoError(err)
	r.Same(p, got)

	again, err := reg.Get("L1")
	r.NoError(err)
	r.Same(p, again)
	r.Equal(1, calls, "the second lookup must use the registered provider")
}

func TestWhenTheLoaderFailsThenGetReturnsItsErrorAndRegistersNothing(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	reg := provider.NewRegistry()
	reg.SetLoader(func(context.Context, string) (provider.StorageProvider, error) {
		return nil, errors.New("token revoked")
	})

	_, err := reg.Get("L1")
	r.ErrorContains(err, "token revoked")
	r.Empty(reg.All(), "a failed rebuild must not register a provider")
}

func TestWhenNoLoaderIsInstalledThenGetReportsTheUnknownLocation(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	_, err := provider.NewRegistry().Get("L1")
	r.ErrorContains(err, `no provider registered for location "L1"`)
}
