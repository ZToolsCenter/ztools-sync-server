package sync

import (
	"context"
	"log"
	"time"
)

// StartDocumentJSONBytesBackfill repairs historical document sizes with bounded, restartable batches.
// Cancellation stops the worker; errors retain the last committed cursor and retry after a delay.
func (s *Service) StartDocumentJSONBytesBackfill(ctx context.Context) {
	go func() {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			complete, err := s.repo.BackfillDocumentJSONBytes(ctx, 200)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				log.Printf("[DocumentBytes] backfill failed: %v", err)
				timer.Reset(30 * time.Second)
				continue
			}
			if complete {
				log.Print("[DocumentBytes] historical document byte backfill complete")
				return
			}
			timer.Reset(time.Second)
		}
	}()
}
