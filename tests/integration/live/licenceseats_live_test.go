//go:build live

package live_test

import (
	"context"
	"fmt"
	"sync"
)

// Licensed users are a fixed, shared resource, so the suite schedules them.
//
// The local Bitbucket runs on an evaluation licence with maximumNumberOfUsers
// of 12. Sequentially that was invisible: a test created two licensed users and
// deleted them before the next one asked. In parallel, twenty tests hold their
// users at the same time, the thirteenth create is refused, and the refusal
// arrives as a 403 on /admin/groups/add-user -- which reads as a permission
// problem and is a capacity one.
//
// A seat is taken before the user is created and returned after it is deleted,
// so the count of licensed users the suite holds can never exceed what the
// licence allows. Tests wait for a seat instead of failing.
var (
	licenceSeats     chan struct{}
	licenceSeatsOnce sync.Once
)

// reservedSeats are the seats that are never the suite's to take: the
// administrator the harness authenticates as, plus one for whatever else the
// instance was configured with.
const reservedSeats = 2

// seatsFromLicence reads the licence and sizes the pool.
//
// Read from the server rather than written down here, because the number is a
// property of the instance the run is pointed at, and a hard-coded 12 would be
// wrong the moment somebody runs against an instance licensed differently.
func (h *liveHarness) seatsFromLicence(ctx context.Context) int {
	const fallbackSeats = 4

	payload, err := h.liveJSON(ctx, "GET", "/rest/api/latest/admin/license", nil)
	if err != nil {
		return fallbackSeats
	}

	maximum, ok := payload["maximumNumberOfUsers"].(float64)
	if !ok {
		// An unlimited licence omits the field, and reports it separately.
		if unlimited, _ := payload["unlimitedNumberOfUsers"].(bool); unlimited {
			return 64
		}

		return fallbackSeats
	}

	seats := int(maximum) - reservedSeats
	if seats < 1 {
		return 1
	}

	return seats
}

// takeLicenceSeat blocks until the licence has room for another user, and
// returns the seat once the test that took it has finished.
func (h *liveHarness) takeLicenceSeat(ctx context.Context) error {
	licenceSeatsOnce.Do(func() {
		licenceSeats = make(chan struct{}, h.seatsFromLicence(ctx))
	})

	select {
	case licenceSeats <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("waiting for a licence seat: %w", ctx.Err())
	}

	// Registered before the user's own cleanup, so it runs after it: cleanups
	// run in reverse, and a seat is only free once the user holding it is gone.
	h.t.Cleanup(func() { <-licenceSeats })

	return nil
}
