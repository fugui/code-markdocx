package vlm

import (
	"context"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

func ProcessBatch(
	ctx context.Context,
	client *Client,
	inputs []ConvertImageInput,
	concurrency int,
	onProgress func(BatchProgress),
) map[string]*VLMResult {
	if concurrency <= 0 {
		concurrency = 3
	}
	total := len(inputs)
	results := make(map[string]*VLMResult, total)
	var mu sync.Mutex
	var doneCount atomic.Int64

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, item := range inputs {
		item := item
		g.Go(func() error {
			res, err := client.ConvertImage(gCtx, item)
			if err == nil && res != nil {
				mu.Lock()
				results[item.ImageID] = res
				mu.Unlock()
			}
			curDone := int(doneCount.Add(1))
			if onProgress != nil {
				onProgress(BatchProgress{
					ImageID: item.ImageID,
					Done:    curDone,
					Total:   total,
					Result:  res,
					Err:     err,
				})
			}
			// Never abort remaining images when a single image fails
			return nil
		})
	}
	_ = g.Wait()
	return results
}
