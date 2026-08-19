// Package contextplan fits one durable session into a bounded
// provider request. It reads a contextstate.Session, decides what
// fits a token window and what does not, and returns a
// provider.Request plus the list of decisions it made. See
// docs/plans/contextplan.md.
package contextplan
