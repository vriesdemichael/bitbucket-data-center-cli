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

// spareSeats is the margin left below the licence ceiling.
//
// The pool used to be the licence maximum minus a flat two, on the assumption
// that the administrator and one other account were the only seats already
// taken. That is a guess, and it was wrong: an instance carrying three
// licensed accounts made the pool ten, ten plus three is thirteen, and the
// thirteenth create came back 403 from a group endpoint -- which reads as a
// permission problem and is a capacity one. The seats already in use are asked
// for now, and this is only the headroom left on top.
const spareSeats = 1

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

	if unlimited, _ := payload["unlimitedNumberOfUsers"].(bool); unlimited {
		return 64
	}

	maximum, ok := payload["maximumNumberOfUsers"].(float64)
	if !ok {
		return fallbackSeats
	}

	// The seats already taken, which the licence reports beside the ceiling.
	// Anything already licensed -- the administrator, accounts the instance was
	// set up with, users an interrupted run left behind -- is not the suite's
	// to hand out.
	taken := 1.0
	if status, _ := payload["status"].(map[string]any); status != nil {
		if current, present := status["currentNumberOfUsers"].(float64); present {
			taken = current
		}
	}

	seats := int(maximum-taken) - spareSeats
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
