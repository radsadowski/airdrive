package index

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWhenAnItemIsAlreadyRunningThenASecondScheduleIsRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()

	r.NoError(db.BeginFileOps(ctx, []FileOp{{ID: "op1", LocationID: "L1", ItemID: "a", ItemPath: "a", Kind: "delete"}}))

	err := db.BeginFileOps(ctx, []FileOp{{ID: "op2", LocationID: "L1", ItemID: "a", ItemPath: "a", Kind: "move"}})
	r.ErrorIs(err, ErrOpInFlight, "the same item cannot be scheduled twice")
}

func TestWhenOneItemOfABatchIsRunningThenTheWholeBatchIsRejected(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()

	r.NoError(db.BeginFileOps(ctx, []FileOp{{ID: "op1", LocationID: "L1", ItemID: "a", Kind: "delete"}}))

	err := db.BeginFileOps(ctx, []FileOp{
		{ID: "op2", LocationID: "L1", ItemID: "b", Kind: "delete"},
		{ID: "op3", LocationID: "L1", ItemID: "a", Kind: "delete"},
	})
	r.ErrorIs(err, ErrOpInFlight)

	r.NoError(db.BeginFileOps(ctx, []FileOp{{ID: "op4", LocationID: "L1", ItemID: "b", Kind: "delete"}}),
		"the rejected batch must not have reserved the free item")
}

func TestWhenTheOperationFinishedThenTheItemCanBeScheduledAgain(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()

	r.NoError(db.BeginFileOps(ctx, []FileOp{{ID: "op1", LocationID: "L1", ItemID: "a", Kind: "delete"}}))
	r.NoError(db.FinishFileOp(ctx, "op1", "done", ""))

	r.NoError(db.BeginFileOps(ctx, []FileOp{{ID: "op2", LocationID: "L1", ItemID: "a", Kind: "delete"}}))
}

func TestWhenTheAppRestartsThenRunningOperationsAreInterrupted(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()

	r.NoError(db.BeginFileOps(ctx, []FileOp{{ID: "op1", LocationID: "L1", ItemID: "a", Kind: "delete"}}))

	n, err := db.InterruptFileOps(ctx, "interrupted by app restart")
	r.NoError(err)
	r.EqualValues(1, n)

	r.NoError(db.BeginFileOps(ctx, []FileOp{{ID: "op2", LocationID: "L1", ItemID: "a", Kind: "delete"}}),
		"an interrupted operation must not block the item forever")
}

func TestWhenFinishedOperationsAreOldThenPruneRemovesThem(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	db := openTestDB(t)
	ctx := context.Background()

	r.NoError(db.BeginFileOps(ctx, []FileOp{
		{ID: "old", LocationID: "L1", ItemID: "a", Kind: "delete"},
		{ID: "live", LocationID: "L1", ItemID: "b", Kind: "delete"},
	}))
	r.NoError(db.FinishFileOp(ctx, "old", "done", ""))

	n, err := db.PruneFileOps(ctx, time.Now().Add(time.Hour))
	r.NoError(err)
	r.EqualValues(1, n, "running rows are never pruned")

	err = db.BeginFileOps(ctx, []FileOp{{ID: "op3", LocationID: "L1", ItemID: "b", Kind: "delete"}})
	r.ErrorIs(err, ErrOpInFlight, "the pruned run still holds its live item")
}
