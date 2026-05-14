package collection

import (
	"context"

	"curio/internal/repository"
)

type CheckResult struct {
	Complete   bool
	LocalCount int
	MovieCount int
}

type Checker struct {
	store *repository.Store
}

func New(store *repository.Store) *Checker {
	return &Checker{store: store}
}

func (c *Checker) Check(ctx context.Context, collectionID, currentMovieID int) (CheckResult, error) {
	parts, err := c.store.CollectionPartIDs(ctx, collectionID)
	if err != nil {
		return CheckResult{}, err
	}
	local, err := c.store.LocalCollectionMovieIDs(ctx, collectionID)
	if err != nil {
		return CheckResult{}, err
	}
	seen := map[int]struct{}{currentMovieID: {}}
	for _, id := range local {
		seen[id] = struct{}{}
	}
	result := CheckResult{MovieCount: len(parts), LocalCount: len(seen)}
	for _, id := range parts {
		if _, ok := seen[id]; !ok {
			result.Complete = false
			return result, nil
		}
	}
	result.Complete = len(parts) > 0
	return result, nil
}
