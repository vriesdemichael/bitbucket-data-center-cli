// Package gateparity holds the test that keeps the local quality gate list and
// the CI quality gate list in step.
//
// It is a test and no command. There is nothing to run in production; the
// invariant is the point, and it is asserted where a broken build will show it.
package gateparity
